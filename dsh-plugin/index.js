// dsh-plugin-ecctl — expose ecctl's public resource surface as typed DSH tools.
//
// This is a trusted Cordis plugin running in the DSH host process. It therefore
// validates every command against `ecctl capabilities`, blocks host-file input,
// and requires one-shot DSH approval before each `ecctl_run` invocation.

import { execFile } from 'node:child_process'
import { createHash } from 'node:crypto'
import z from '@deepseek-ai/schemastery'
import { defineTool } from '@deepseek-ai/dsh-tools'

export const name = 'ecctl'

const DEFAULT_TIMEOUT_MS = 65 * 60 * 1000
const DEFAULT_MAX_BUFFER_BYTES = 16 * 1024 * 1024
const MAX_COMMAND_TOKENS = 32
const MAX_EXTRA_TOKENS = 256
const MAX_FILTERS = 100
const MAX_TAGS = 100
const MAX_INPUT_BYTES = 1024 * 1024

const OWNED_FLAGS = new Set([
  'agent-envelope',
  'api-param',
  'dry-run',
  'filter',
  'help',
  'idempotency-key',
  'json',
  'lang',
  'no-color',
  'output',
  'region',
  'request',
  'show-secret',
  'tag',
  'version',
])

export const Config = z.object({
  bin: z.string().default('ecctl').description('Path or name of the ecctl executable.'),
  timeoutMs: z.natural().min(1).default(DEFAULT_TIMEOUT_MS)
    .description('Hard timeout for one ecctl child process (ms).'),
  maxOutputBytes: z.natural().min(1).default(DEFAULT_MAX_BUFFER_BYTES)
    .description('Maximum bytes captured independently from stdout and stderr.'),
})

export const inject = ['tools']

function childEnv() {
  return { ...process.env, ECCTL_DISABLE_UPDATE_CHECK: '1' }
}

function runEcctl(config, argv, signal) {
  return new Promise((resolve) => {
    execFile(config.bin, argv, {
      timeout: config.timeoutMs,
      maxBuffer: config.maxOutputBytes,
      env: childEnv(),
      signal,
    }, (error, stdout, stderr) => {
      resolve({
        code: error ? (typeof error.code === 'number' ? error.code : null) : 0,
        errorCode: error && typeof error.code !== 'number' ? String(error.code ?? '') : '',
        errorName: error?.name ?? '',
        killed: error?.killed === true,
        signal: error?.signal ?? undefined,
        stdout: error?.stdout != null ? String(error.stdout) : String(stdout ?? ''),
        stderr: error?.stderr != null
          ? String(error.stderr)
          : String(stderr ?? '') || String(error?.message ?? ''),
      })
    })
  })
}

function failureText(result, mutationContext) {
  const body = result.stdout.trim()
  const errorBody = result.stderr.trim()
  const outcomeUnknown = mutationContext.mutating && (
    result.code !== 0 ||
    result.killed ||
    Boolean(result.signal) ||
    result.errorCode === 'ERR_CHILD_PROCESS_STDIO_MAXBUFFER'
  )
  const status = (result.code ?? result.errorCode) || 'unknown'
  const parts = []
  if (outcomeUnknown) {
    parts.push(
      `ecctl mutation outcome is unknown after child termination (${status}). ` +
      'Do not retry until the resource state has been reconciled.',
    )
  } else {
    parts.push(`ecctl failed with status ${status}${result.signal ? ` / signal ${result.signal}` : ''}.`)
  }
  if (mutationContext.idempotencyKey) {
    parts.push(`Invocation idempotency key: ${mutationContext.idempotencyKey}.`)
  }
  if (body) parts.push(body)
  if (errorBody) parts.push(`[stderr]\n${errorBody}`)
  return parts.join('\n')
}

function successfulText(result, mutationContext = {}) {
  if (result.code !== 0) throw new Error(failureText(result, mutationContext))
  return result.stdout.trim()
}

function successfulJSON(result, mutationContext = {}) {
  const raw = successfulText(result, mutationContext)
  try {
    return JSON.parse(raw)
  } catch (error) {
    const prefix = mutationContext.mutating
      ? 'ecctl returned invalid JSON after a mutation; the outcome is unknown. '
      : 'ecctl returned invalid JSON. '
    throw new Error(`${prefix}Do not retry until the result is reconciled: ${error.message}`)
  }
}

function renderText(_args, value) {
  return [{ type: 'text', text: value }]
}

function renderJSON(_args, value) {
  return [{ type: 'text', text: JSON.stringify(value) }]
}

function validateArray(name, values, maxItems) {
  if (!Array.isArray(values) || values.length === 0) {
    throw new TypeError(`${name} must contain at least one value`)
  }
  if (values.length > maxItems) {
    throw new TypeError(`${name} accepts at most ${maxItems} values`)
  }
  for (const value of values) {
    if (typeof value !== 'string' || value.length === 0) {
      throw new TypeError(`${name} values must be non-empty strings`)
    }
  }
}

function inputSize(input) {
  const values = [
    ...(input.command ?? []),
    ...(input.filters ?? []),
    ...(input.tags ?? []),
    ...(input.extra_args ?? []),
    input.region ?? '',
    input.config_profile ?? '',
    input.lang ?? '',
  ]
  return values.reduce((total, value) => total + Buffer.byteLength(value), 0)
}

function validateInputBudget(input) {
  validateArray('command', input.command, MAX_COMMAND_TOKENS)
  if ((input.extra_args?.length ?? 0) > MAX_EXTRA_TOKENS) {
    throw new TypeError(`extra_args accepts at most ${MAX_EXTRA_TOKENS} values`)
  }
  if ((input.filters?.length ?? 0) > MAX_FILTERS) {
    throw new TypeError(`filters accepts at most ${MAX_FILTERS} values`)
  }
  if ((input.tags?.length ?? 0) > MAX_TAGS) {
    throw new TypeError(`tags accepts at most ${MAX_TAGS} values`)
  }
  if (input.command.some((token) => token.startsWith('-'))) {
    throw new TypeError('command accepts positional resource-operation tokens only')
  }
  if (inputSize(input) > MAX_INPUT_BYTES) {
    throw new TypeError(`ecctl_run input exceeds ${MAX_INPUT_BYTES} bytes`)
  }
}

function commandCatalog(capabilities) {
  if (!Array.isArray(capabilities?.products)) {
    throw new TypeError('ecctl capabilities response has no products array')
  }
  const commands = []
  for (const product of capabilities.products) {
    if (typeof product?.product !== 'string' || !Array.isArray(product.resources)) continue
    for (const resource of product.resources) {
      if (typeof resource?.name !== 'string' || typeof resource.schema_id !== 'string') continue
      for (const action of resource.actions ?? []) {
        if (typeof action !== 'string') continue
        const prefix = resource.parent
          ? [product.product, resource.parent, resource.name, action]
          : resource.name === product.product
            ? [product.product, action]
            : [product.product, resource.name, action]
        commands.push({ prefix, schemaName: `${resource.schema_id}.${action}` })
      }
    }
  }
  commands.sort((a, b) => b.prefix.length - a.prefix.length)
  return commands
}

function matchPublicCommand(tokens, commands) {
  const match = commands.find(({ prefix }) =>
    prefix.every((token, index) => tokens[index] === token),
  )
  if (!match) {
    throw new TypeError(
      'command must start with a public resource operation exposed by ecctl capabilities',
    )
  }
  return match
}

function flagName(token) {
  if (!token.startsWith('--') || token === '--') return ''
  return token.slice(2).split('=', 1)[0].toLowerCase()
}

function hasAtFileValue(token) {
  const equals = token.indexOf('=')
  const value = equals >= 0 ? token.slice(equals + 1) : token
  return value.trimStart().startsWith('@')
}

function validateExtraArgs(input, schema) {
  const args = input.extra_args ?? []
  const resourceProfile = Object.prototype.hasOwnProperty.call(schema.params ?? {}, 'profile')
  for (const token of args) {
    if (typeof token !== 'string' || token.length === 0) {
      throw new TypeError('extra_args values must be non-empty strings')
    }
    if (token === '--' || (token.startsWith('-') && !token.startsWith('--'))) {
      throw new TypeError(`extra_args may not contain ${JSON.stringify(token)}`)
    }
    const flag = flagName(token)
    if (OWNED_FLAGS.has(flag) || (flag === 'profile' && !resourceProfile)) {
      throw new TypeError(`extra_args may not override plugin-owned flag --${flag}`)
    }
    if (hasAtFileValue(token)) {
      throw new TypeError('@file inputs are blocked because the plugin runs outside the DSH sandbox')
    }
  }
}

function globalFlags(input) {
  const flags = []
  if (input.region) flags.push('--region', input.region)
  if (input.config_profile) flags.push('--profile', input.config_profile)
  if (input.lang) flags.push('--lang', input.lang)
  return flags
}

function stableIdempotencyKey(exec, operation, input) {
  if (!operation.schema?.contract?.idempotency?.supported || input.dry_run) return ''
  const digest = createHash('sha256')
    .update(JSON.stringify([
      exec.agent?.id ?? '',
      exec.callId,
      operation.schemaName,
      input.command,
      input.region ?? '',
      input.config_profile ?? '',
      input.lang ?? '',
      input.filters ?? [],
      input.tags ?? [],
      input.dry_run === true,
      input.extra_args ?? [],
    ]))
    .digest('hex')
    .slice(0, 32)
  return `dsh-${digest}`
}

function assembleRun(input, operation, exec) {
  validateExtraArgs(input, operation.schema)
  const argv = [...globalFlags(input), ...input.command]
  for (const filter of input.filters ?? []) argv.push('--filter', filter)
  for (const tag of input.tags ?? []) argv.push('--tag', tag)
  if (input.dry_run) argv.push('--dry-run')
  argv.push(...(input.extra_args ?? []))
  const idempotencyKey = stableIdempotencyKey(exec, operation, input)
  if (idempotencyKey) argv.push('--idempotency-key', idempotencyKey)
  argv.push('--output', 'json', '--no-color')
  return { argv, idempotencyKey }
}

function approvalSummary(input, operation) {
  const command = input.command.map((token) =>
    token.length > 160 ? `${token.slice(0, 157)}...` : token,
  )
  const extraFlags = [...new Set(
    (input.extra_args ?? []).map(flagName).filter(Boolean).map((flag) => `--${flag}`),
  )]
  const profile = (input.config_profile || 'default').slice(0, 160)
  const region = (input.region || 'default').slice(0, 160)
  const risk = operation.schema?.risk?.level ?? operation.schema?.kind ?? 'unknown'
  return (
    `Run ${operation.schemaName} with risk ${risk}; ` +
    `command=${JSON.stringify(command)}, extra_flags=${JSON.stringify(extraFlags)}, ` +
    `dry_run=${input.dry_run === true}, profile=${JSON.stringify(profile)}, ` +
    `region=${JSON.stringify(region)}.`
  )
}

async function requireApproval(ctx, exec, input, operation) {
  const approval = ctx.get('approval')
  if (!approval?.request || !exec.agent) {
    throw new Error('ecctl_run requires the DSH approval service and an active agent turn')
  }
  const outcome = await approval.request({
    agent: exec.agent,
    toolName: 'ecctl_run',
    callId: exec.callId,
    signal: exec.signal,
    reason: approvalSummary(input, operation),
  })
  if (exec.signal.aborted) return false
  if (outcome !== 'allowed-once') {
    throw new Error(`ecctl_run approval was not granted (${outcome})`)
  }
  return true
}

export function apply(ctx, config) {
  if (typeof config.bin !== 'string' || config.bin.trim() === '') {
    throw new TypeError('config.bin must be a non-empty string')
  }

  let commands
  const schemas = new Map()

  async function getCommands(exec) {
    if (commands) return commands
    const result = await runEcctl(
      config,
      ['capabilities', '--lang', 'en', '--output', 'json', '--no-color'],
      exec.signal,
    )
    if (exec.signal.aborted) return null
    commands = commandCatalog(successfulJSON(result))
    return commands
  }

  async function getOperation(input, exec) {
    validateInputBudget(input)
    const publicCommands = await getCommands(exec)
    if (!publicCommands) return null
    const match = matchPublicCommand(input.command, publicCommands)
    let schema = schemas.get(match.schemaName)
    if (!schema) {
      const result = await runEcctl(
        config,
        ['schema', match.schemaName, '--brief', '--lang', 'en', '--output', 'json', '--no-color'],
        exec.signal,
      )
      if (exec.signal.aborted) return null
      const response = successfulJSON(result)
      schema = response[match.schemaName] ?? (
        response.command === match.schemaName ? response : undefined
      )
      if (!schema || typeof schema !== 'object') {
        throw new TypeError(`ecctl schema response omitted ${match.schemaName}`)
      }
      schemas.set(match.schemaName, schema)
    }
    if (input.dry_run && schema.contract?.dry_run?.supported !== true) {
      throw new TypeError(`${match.schemaName} does not support --dry-run`)
    }
    if (schema.params?.region?.required === true && !input.region) {
      throw new TypeError(`${match.schemaName} requires an explicit region`)
    }
    validateExtraArgs(input, schema)
    return { ...match, schema }
  }

  ctx.tools.register(defineTool({
    name: 'ecctl_version',
    description: 'Print the installed ecctl CLI version.',
    parameters: {},
    output: { schema: { type: 'string' }, render: renderText },
    async execute(_args, exec) {
      const result = await runEcctl(config, ['--version'], exec.signal)
      if (exec.signal.aborted) return ''
      return successfulText(result)
    },
  }))

  ctx.tools.register(defineTool({
    name: 'ecctl_capabilities',
    description: 'Return ecctl\'s machine-readable public product/resource/action catalog.',
    parameters: {
      lang: { type: 'string', enum: ['en', 'zh-CN'], description: 'Display language.' },
    },
    output: { schema: { type: 'json' }, render: renderJSON },
    async execute(args, exec) {
      const argv = ['capabilities', '--output', 'json', '--no-color']
      if (args.lang) argv.push('--lang', args.lang)
      const result = await runEcctl(config, argv, exec.signal)
      if (exec.signal.aborted) return null
      return successfulJSON(result)
    },
  }))

  ctx.tools.register(defineTool({
    name: 'ecctl_schema',
    description: 'Inspect compact JSON schemas for one or more public ecctl resource commands.',
    parameters: {
      names: {
        type: 'array',
        items: { type: 'string' },
        required: true,
        description: 'Command schema names, e.g. ["ecs.instance.list"].',
      },
      full: { type: 'boolean', description: 'Show all schema-visible parameters.' },
      lang: { type: 'string', enum: ['en', 'zh-CN'], description: 'Display language.' },
    },
    output: { schema: { type: 'json' }, render: renderJSON },
    async execute(args, exec) {
      validateArray('names', args.names, 64)
      const argv = ['schema', ...args.names]
      if (args.full) argv.push('--full')
      argv.push('--output', 'json', '--no-color')
      if (args.lang) argv.push('--lang', args.lang)
      const result = await runEcctl(config, argv, exec.signal)
      if (exec.signal.aborted) return null
      return successfulJSON(result)
    },
  }))

  ctx.tools.register(defineTool({
    name: 'ecctl_run',
    description:
      'Run one public ecctl resource operation after fail-closed DSH approval. ' +
      'The command is validated against capabilities; host configuration, raw call, ' +
      'update, and @file inputs are blocked. Use ecctl_schema first.',
    parameters: {
      command: {
        type: 'array',
        items: { type: 'string' },
        required: true,
        description: 'Canonical resource-operation tokens plus positional IDs.',
      },
      region: { type: 'string', description: 'Explicit Alibaba Cloud region.' },
      config_profile: { type: 'string', description: 'ecctl credential/config profile.' },
      lang: { type: 'string', enum: ['en', 'zh-CN'], description: 'Display language.' },
      filters: {
        type: 'array',
        items: { type: 'string' },
        description: 'Repeated --filter key=value expressions.',
      },
      tags: {
        type: 'array',
        items: { type: 'string' },
        description: 'Repeated --tag key=value assignments.',
      },
      dry_run: { type: 'boolean', description: 'Require operation-supported --dry-run.' },
      extra_args: {
        type: 'array',
        items: { type: 'string' },
        description: 'Resource-specific inline arguments; plugin-owned flags and @file are rejected.',
      },
    },
    output: { schema: { type: 'json' }, render: renderJSON },
    async execute(args, exec) {
      const operation = await getOperation(args, exec)
      if (!operation || exec.signal.aborted) return null
      if (!await requireApproval(ctx, exec, args, operation)) return null
      const assembled = assembleRun(args, operation, exec)
      const mutating = operation.schema.kind === 'mutation' && !args.dry_run
      const result = await runEcctl(config, assembled.argv, exec.signal)
      if (exec.signal.aborted) {
        if (mutating) {
          throw new Error(failureText(result, {
            mutating: true,
            idempotencyKey: assembled.idempotencyKey,
          }))
        }
        return null
      }
      return successfulJSON(result, {
        mutating,
        idempotencyKey: assembled.idempotencyKey,
      })
    },
  }))
}

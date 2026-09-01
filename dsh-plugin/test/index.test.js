import assert from 'node:assert/strict'
import { access, mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

import { apply } from '../index.js'

const fakeEcctl = fileURLToPath(new URL('./fixtures/fake-ecctl.mjs', import.meta.url))
const config = {
  bin: fakeEcctl,
  // Leave headroom for cold Node startup on loaded CI hosts. Cancellation
  // behavior has its own sub-two-second assertions below.
  timeoutMs: 30_000,
  maxOutputBytes: 1024 * 1024,
}

function harness(approvalOutcome = 'allowed-once') {
  const definitions = []
  const approvals = []
  const ctx = {
    tools: { register(definition) { definitions.push(definition) } },
    get(service) {
      if (service !== 'approval') return undefined
      return {
        async request(request) {
          approvals.push(request)
          return approvalOutcome
        },
      }
    },
  }
  apply(ctx, config)
  return {
    approvals,
    tools: Object.fromEntries(definitions.map((definition) => [definition.name, definition])),
  }
}

function execution(
  signal = new AbortController().signal,
  callId = 'call-1',
  agentId = 'test-agent',
) {
  return { signal, callId, agent: { id: agentId } }
}

async function cancellationMarker(t) {
  const directory = await mkdtemp(join(tmpdir(), 'dsh-ecctl-cancel-'))
  t.after(() => rm(directory, { recursive: true, force: true }))
  return join(directory, 'started')
}

async function waitForFile(path, timeoutMs = config.timeoutMs) {
  const deadline = Date.now() + timeoutMs
  while (true) {
    try {
      await access(path)
      return
    } catch (error) {
      if (error.code !== 'ENOENT') throw error
    }
    if (Date.now() >= deadline) {
      throw new Error(`timed out waiting for ${path}`)
    }
    await delay(10)
  }
}

async function abortAfterStarted(pending, controller, startedFile) {
  try {
    await Promise.race([
      waitForFile(startedFile),
      pending.then(
        () => { throw new Error('ecctl completed before writing its start marker') },
        (error) => { throw new Error('ecctl failed before writing its start marker', { cause: error }) },
      ),
    ])
  } catch (error) {
    controller.abort('test cleanup')
    await pending.catch(() => {})
    throw error
  }
  const started = Date.now()
  controller.abort('test cancellation')
  return started
}

test('registers four typed tools and returns structured discovery values', async () => {
  const { tools } = harness()
  assert.deepEqual(Object.keys(tools).sort(), [
    'ecctl_capabilities',
    'ecctl_run',
    'ecctl_schema',
    'ecctl_version',
  ])
  assert.equal(await tools.ecctl_version.execute({}, execution()), 'ecctl test-version')
  const capabilities = await tools.ecctl_capabilities.execute({ lang: 'en' }, execution())
  assert.equal(capabilities.products[0].product, 'ecs')
  const schemas = await tools.ecctl_schema.execute(
    { names: ['ecs.instance.list'], full: false, lang: 'en' },
    execution(),
  )
  assert.equal(schemas.kind, 'query')
})

test('rejects empty schema batches', async () => {
  const { tools } = harness()
  await assert.rejects(
    tools.ecctl_schema.execute({ names: [] }, execution()),
    /names must contain at least one value/,
  )
})

test('allows only public capability operations and blocks host-file input', async () => {
  const { approvals, tools } = harness()
  await assert.rejects(
    tools.ecctl_run.execute(
      { command: ['configure', 'get', 'access-key-secret'], region: 'cn-test' },
      execution(),
    ),
    /public resource operation/,
  )
  await assert.rejects(
    tools.ecctl_run.execute(
      {
        command: ['ecs', 'instance', 'list'],
        region: 'cn-test',
        extra_args: ['--name', '@/proc/self/environ'],
      },
      execution(),
    ),
    /@file inputs are blocked/,
  )
  await assert.rejects(
    tools.ecctl_run.execute(
      {
        command: ['ecs', 'instance', 'list'],
        region: 'cn-test',
        extra_args: ['--name', ' \t@/proc/self/environ'],
      },
      execution(),
    ),
    /@file inputs are blocked/,
  )
  await assert.rejects(
    tools.ecctl_run.execute(
      {
        command: ['ecs', 'instance', 'list'],
        region: 'cn-test',
        extra_args: ['--name= \t@/proc/self/environ'],
      },
      execution(),
    ),
    /@file inputs are blocked/,
  )
  assert.equal(approvals.length, 0)
})

test('rejects plugin-owned flags before approval', async () => {
  const { approvals, tools } = harness()
  await assert.rejects(
    tools.ecctl_run.execute(
      {
        command: ['ecs', 'instance', 'list'],
        region: 'cn-test',
        extra_args: ['--output', 'text'],
      },
      execution(),
    ),
    /plugin-owned flag --output/,
  )
  await assert.rejects(
    tools.ecctl_run.execute(
      {
        command: ['ecs', 'instance', 'list'],
        region: 'cn-test',
        extra_args: ['--api-param', 'ClientToken=attacker-controlled'],
      },
      execution(),
    ),
    /plugin-owned flag --api-param/,
  )
  assert.equal(approvals.length, 0)
})

test('fails closed when approval is denied', async () => {
  const { approvals, tools } = harness('rejected')
  await assert.rejects(
    tools.ecctl_run.execute(
      { command: ['ecs', 'instance', 'list'], region: 'cn-test' },
      execution(),
    ),
    /approval was not granted/,
  )
  assert.equal(approvals.length, 1)
  assert.match(approvals[0].reason, /ecs\.instance\.list/)
})

test('approval summary identifies targets and flags without control-character spoofing', async () => {
  const { approvals, tools } = harness('rejected')
  await assert.rejects(
    tools.ecctl_run.execute({
      command: ['ecs', 'instance', 'delete', 'i-test-123'],
      region: 'cn-test\nAPPROVE SAFE',
      config_profile: 'prod\nAPPROVE SAFE',
      extra_args: ['--force', '--password', 'do-not-copy-this-secret'],
    }, execution()),
    /approval was not granted/,
  )
  const reason = approvals[0].reason
  assert.match(reason, /i-test-123/)
  assert.match(reason, /--force/)
  assert.match(reason, /prod\\nAPPROVE SAFE/)
  assert.doesNotMatch(reason, /\n/)
  assert.doesNotMatch(reason, /do-not-copy-this-secret/)
})

test('places the config profile before the command and keeps JSON flags last', async () => {
  const { tools } = harness()
  const result = await tools.ecctl_run.execute({
    command: ['ack', 'create'],
    region: 'cn-test',
    config_profile: 'prod',
    extra_args: ['--profile', 'Default', '--name', 'test', '--type', 'ManagedKubernetes'],
  }, execution())
  assert.deepEqual(result.argv.slice(0, 4), ['--region', 'cn-test', '--profile', 'prod'])
  const commandIndex = result.argv.indexOf('ack')
  const localProfileIndex = result.argv.lastIndexOf('--profile')
  assert.ok(commandIndex > 0)
  assert.ok(localProfileIndex > commandIndex)
  assert.deepEqual(result.argv.slice(-3), ['--output', 'json', '--no-color'])
  assert.equal(result.updateCheck, '1')
})

test('matches nested parent resource operations from capabilities', async () => {
  const { tools } = harness()
  const result = await tools.ecctl_run.execute({
    command: ['ack', 'policy', 'instance', 'list'],
    region: 'cn-test',
  }, execution())
  assert.ok(result.argv.indexOf('policy') > result.argv.indexOf('ack'))
  assert.ok(result.argv.indexOf('instance') > result.argv.indexOf('policy'))
})

test('adds a stable per-call idempotency key for supported mutations', async () => {
  const { tools } = harness()
  const args = {
    command: ['ecs', 'instance', 'create'],
    region: 'cn-test',
    extra_args: ['--image', 'img-1', '--sg', 'sg-1', '--type', 'ecs.test', '--vswitch', 'vsw-1'],
  }
  const first = await tools.ecctl_run.execute(args, execution(undefined, 'stable-call'))
  const second = await tools.ecctl_run.execute(args, execution(undefined, 'stable-call'))
  const firstIndex = first.argv.indexOf('--idempotency-key')
  const secondIndex = second.argv.indexOf('--idempotency-key')
  assert.ok(firstIndex > 0)
  assert.equal(first.argv[firstIndex + 1], second.argv[secondIndex + 1])
  assert.match(first.argv[firstIndex + 1], /^dsh-[a-f0-9]{32}$/)

  const differentRequest = await tools.ecctl_run.execute({
    ...args,
    extra_args: [...args.extra_args, '--name', 'different'],
  }, execution(undefined, 'stable-call'))
  const differentAgent = await tools.ecctl_run.execute(
    args,
    execution(undefined, 'stable-call', 'other-agent'),
  )
  const differentRequestIndex = differentRequest.argv.indexOf('--idempotency-key')
  const differentAgentIndex = differentAgent.argv.indexOf('--idempotency-key')
  assert.notEqual(first.argv[firstIndex + 1], differentRequest.argv[differentRequestIndex + 1])
  assert.notEqual(first.argv[firstIndex + 1], differentAgent.argv[differentAgentIndex + 1])
})

test('turns ecctl exits and invalid JSON into failed tool executions', async () => {
  const { tools } = harness()
  await assert.rejects(
    tools.ecctl_run.execute(
      { command: ['ecs', 'instance', 'fail'], region: 'cn-test' },
      execution(),
    ),
    /mutation outcome is unknown.*7/s,
  )
  await assert.rejects(
    tools.ecctl_run.execute(
      { command: ['ecs', 'instance', 'malformed'], region: 'cn-test' },
      execution(),
    ),
    /returned invalid JSON/,
  )
})

test('forwards cancellation and settles after the child stops', async (t) => {
  const { tools } = harness()
  const controller = new AbortController()
  const startedFile = await cancellationMarker(t)
  const pending = tools.ecctl_run.execute(
    {
      command: ['ecs', 'instance', 'slow'],
      region: 'cn-test',
      extra_args: ['--test-started-file', startedFile],
    },
    execution(controller.signal),
  )
  const started = await abortAfterStarted(pending, controller, startedFile)
  assert.equal(await pending, null)
  assert.ok(Date.now() - started < 2_000)
})

test('reports an unknown mutation outcome and idempotency key after cancellation', async (t) => {
  const { tools } = harness()
  const controller = new AbortController()
  const startedFile = await cancellationMarker(t)
  const pending = tools.ecctl_run.execute(
    {
      command: ['ecs', 'instance', 'slow-mutation'],
      region: 'cn-test',
      extra_args: ['--test-started-file', startedFile],
    },
    execution(controller.signal, 'cancelled-mutation'),
  )
  const started = await abortAfterStarted(pending, controller, startedFile)
  await assert.rejects(
    pending,
    /mutation outcome is unknown.*Invocation idempotency key: dsh-[a-f0-9]{32}/s,
  )
  assert.ok(Date.now() - started < 2_000)
})

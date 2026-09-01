#!/usr/bin/env node

import { writeFileSync } from 'node:fs'

const args = process.argv.slice(2)

if (args.includes('--version')) {
  process.stdout.write('ecctl test-version\n')
  process.exit(0)
}

if (args[0] === 'capabilities') {
  process.stdout.write(JSON.stringify({
    products: [
      {
        product: 'ecs',
        resources: [{
          name: 'instance',
          schema_id: 'ecs.instance',
          actions: ['list', 'create', 'delete', 'fail', 'slow', 'slow-mutation', 'malformed'],
        }],
      },
      {
        product: 'ack',
        resources: [
          {
            name: 'ack',
            schema_id: 'ack.ack',
            actions: ['create'],
          },
          {
            name: 'instance',
            parent: 'policy',
            schema_id: 'ack.policy.instance',
            actions: ['list'],
          },
        ],
      },
    ],
  }))
  process.exit(0)
}

const schemas = {
  'ecs.instance.list': {
    kind: 'query',
    risk: { level: 'low' },
    params: { region: { required: true } },
    contract: { dry_run: { supported: false }, idempotency: { supported: false } },
  },
  'ecs.instance.create': {
    kind: 'mutation',
    risk: { level: 'medium' },
    params: { region: { required: true } },
    contract: { dry_run: { supported: true }, idempotency: { supported: true } },
  },
  'ecs.instance.delete': {
    kind: 'mutation',
    risk: { level: 'high' },
    params: { region: { required: true } },
    contract: { dry_run: { supported: false }, idempotency: { supported: false } },
  },
  'ecs.instance.fail': {
    kind: 'mutation',
    risk: { level: 'medium' },
    params: { region: { required: true } },
    contract: { dry_run: { supported: false }, idempotency: { supported: false } },
  },
  'ecs.instance.slow': {
    kind: 'query',
    risk: { level: 'low' },
    params: { region: { required: true } },
    contract: { dry_run: { supported: false }, idempotency: { supported: false } },
  },
  'ecs.instance.slow-mutation': {
    kind: 'mutation',
    risk: { level: 'medium' },
    params: { region: { required: true } },
    contract: { dry_run: { supported: false }, idempotency: { supported: true } },
  },
  'ecs.instance.malformed': {
    kind: 'query',
    risk: { level: 'low' },
    params: { region: { required: true } },
    contract: { dry_run: { supported: false }, idempotency: { supported: false } },
  },
  'ack.ack.create': {
    kind: 'mutation',
    risk: { level: 'medium' },
    params: { profile: {}, region: { required: true } },
    contract: { dry_run: { supported: true }, idempotency: { supported: true } },
  },
  'ack.policy.instance.list': {
    kind: 'query',
    risk: { level: 'low' },
    params: { region: { required: true } },
    contract: { dry_run: { supported: false }, idempotency: { supported: false } },
  },
}

if (args[0] === 'schema') {
  const names = []
  for (const arg of args.slice(1)) {
    if (arg.startsWith('-')) break
    names.push(arg)
  }
  const result = {}
  for (const name of names) {
    if (schemas[name]) result[name] = { command: name, ...schemas[name] }
  }
  process.stdout.write(JSON.stringify(names.length === 1 ? result[names[0]] : result))
  process.exit(0)
}

const root = args.findIndex((arg) => arg === 'ecs' || arg === 'ack')
const action = root >= 0 && args[root] === 'ack' ? args[root + 1] : args[root + 2]

if (action === 'fail') {
  process.stdout.write(JSON.stringify({ error: { code: 'FakeFailure' } }))
  process.exit(7)
}

if (action === 'malformed') {
  process.stdout.write('not-json')
  process.exit(0)
}

if (action === 'slow' || action === 'slow-mutation') {
  const startedFileIndex = args.indexOf('--test-started-file')
  if (startedFileIndex >= 0 && args[startedFileIndex + 1]) {
    writeFileSync(args[startedFileIndex + 1], action, { flag: 'wx' })
  }
  setTimeout(() => process.stdout.write(JSON.stringify({ argv: args })), 10_000)
} else {
  process.stdout.write(JSON.stringify({
    argv: args,
    updateCheck: process.env.ECCTL_DISABLE_UPDATE_CHECK,
  }))
}

import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {
  lstat,
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  rm,
  stat,
  symlink,
  writeFile,
} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import test from 'node:test';
import {fileURLToPath} from 'node:url';

const websiteDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const docs = [
  'docs/user-guide/resource-optimizations/ecs.md',
  'i18n/zh-Hans/docusaurus-plugin-content-docs/current/user-guide/resource-optimizations/ecs.md',
];

async function keypairExample(relativePath) {
  const content = await readFile(path.join(websiteDir, relativePath), 'utf8');
  const marker = content.indexOf('private_key_file=./web-key.pem');
  assert.notEqual(marker, -1, `missing private-key save example in ${relativePath}`);
  const fence = content.lastIndexOf('```bash\n', marker);
  const start = fence + '```bash\n'.length;
  const end = content.indexOf('\n```', marker);
  return content.slice(start, end);
}

async function withFakeEcctl(payload, callback) {
  const root = await mkdtemp(path.join(tmpdir(), 'ecctl-keypair-workflow-'));
  const bin = path.join(root, 'bin');
  const called = path.join(root, 'ecctl-called');
  await mkdir(bin);
  const quotedPayload = payload.replaceAll("'", "'\"'\"'");
  await writeFile(
    path.join(bin, 'ecctl'),
    `#!/bin/sh
if [ -n "$FAKE_ECCTL_CALLED" ]; then
  : > "$FAKE_ECCTL_CALLED"
fi
if [ -n "$FAKE_ECCTL_CREATE_TARGET" ]; then
  printf 'racer' > "$FAKE_ECCTL_CREATE_TARGET"
fi
printf '%s' '${quotedPayload}'
if [ -n "$FAKE_ECCTL_EXIT_CODE" ]; then
  exit "$FAKE_ECCTL_EXIT_CODE"
fi
`,
    {mode: 0o700},
  );

  const run = (example, overrides = {}) =>
    spawnSync('bash', ['-c', example], {
      cwd: root,
      encoding: 'utf8',
      env: {
        ...process.env,
        FAKE_ECCTL_CALLED: called,
        PATH: `${bin}:${process.env.PATH ?? ''}`,
        TMPDIR: root,
        ...overrides,
      },
    });

  try {
    await callback({called, root, run});
  } finally {
    await rm(root, {recursive: true, force: true});
  }
}

async function assertMissing(filePath) {
  await assert.rejects(stat(filePath), (error) => error.code === 'ENOENT');
}

test('keypair private-key examples protect the one-time secret before creation', async () => {
  const examples = await Promise.all(docs.map(keypairExample));
  assert.equal(examples[1], examples[0], 'English and Chinese workflows must stay identical');

  for (const [index, example] of examples.entries()) {
    const relativePath = docs[index];

    assert.match(example, /command -v jq/);
    assert.match(example, /\[ -e "\$private_key_file" \] \|\| \[ -L "\$private_key_file" \]/);
    const responseTemplate = example.match(
      /response_file="\$\(mktemp "\$\{TMPDIR:-\/tmp\}\/([^"]+)"\)"/,
    )?.[1];
    assert.match(responseTemplate ?? '', /XXXXXX$/);
    assert.match(example, /private_key_temp="\$\(mktemp /);
    assert.match(example, /--output json > "\$response_file"/);
    assert.match(example, /jq -ejr/);
    assert.match(example, /select\(type == "string" and length > 0\)/);
    assert.match(example, /ln "\$private_key_temp" "\$private_key_file"/);
    assert.match(example, /response retained at %s/);
    assert.doesNotMatch(example, /--name web-key \|/);

    const preflight = example.indexOf('command -v jq');
    const destinationCheck = example.indexOf('[ -e "$private_key_file" ]');
    const invocation = example.indexOf('ecctl ecs keypair create');
    assert.ok(preflight < invocation, `jq must be checked before creation in ${relativePath}`);
    assert.ok(
      destinationCheck < invocation,
      `the destination must be checked before creation in ${relativePath}`,
    );

    const syntax = spawnSync('bash', ['-n'], {input: example, encoding: 'utf8'});
    assert.equal(syntax.status, 0, `invalid Bash example in ${relativePath}: ${syntax.stderr}`);

    const tempRoot = await mkdtemp(path.join(tmpdir(), 'ecctl-keypair-docs-'));
    try {
      const generated = spawnSync('mktemp', [path.join(tempRoot, responseTemplate)], {
        encoding: 'utf8',
      });
      assert.equal(
        generated.status,
        0,
        `response mktemp failed in ${relativePath}: ${generated.stderr}`,
      );
      const generatedPath = generated.stdout.trim();
      assert.notEqual(generatedPath, path.join(tempRoot, responseTemplate));
      assert.doesNotMatch(path.basename(generatedPath), /XXXXXX/);
    } finally {
      await rm(tempRoot, {recursive: true, force: true});
    }
  }
});

test('keypair private-key workflow preserves exact PEM bytes and private permissions', async () => {
  const example = await keypairExample(docs[0]);
  const privateKey =
    '-----BEGIN PRIVATE KEY-----\nprivate-key-material\n-----END PRIVATE KEY-----\n';
  const payload = JSON.stringify({keypair: {private_key: privateKey}});

  await withFakeEcctl(payload, async ({called, root, run}) => {
    const result = run(example);
    assert.equal(result.status, 0, result.stderr);
    assert.equal(await readFile(path.join(root, 'web-key.pem'), 'utf8'), privateKey);
    assert.equal((await stat(path.join(root, 'web-key.pem'))).mode & 0o777, 0o600);
    await stat(called);

    const names = await readdir(root);
    assert.equal(names.some((name) => name.startsWith('ecctl-keypair-response.json.')), false);
    assert.equal(names.some((name) => name.startsWith('web-key.pem.')), false);
  });
});

test('keypair private-key workflow refuses existing and dangling-symlink targets before creation', async () => {
  const example = await keypairExample(docs[0]);
  const payload = JSON.stringify({keypair: {private_key: 'secret'}});

  await withFakeEcctl(payload, async ({called, root, run}) => {
    const target = path.join(root, 'web-key.pem');
    await writeFile(target, 'keep', {mode: 0o600});
    const existing = run(example);
    assert.notEqual(existing.status, 0);
    assert.equal(await readFile(target, 'utf8'), 'keep');
    await assertMissing(called);
  });

  await withFakeEcctl(payload, async ({called, root, run}) => {
    const target = path.join(root, 'web-key.pem');
    await symlink('missing-private-key.pem', target);
    const dangling = run(example);
    assert.notEqual(dangling.status, 0);
    assert.equal((await lstat(target)).isSymbolicLink(), true);
    await assertMissing(called);
  });
});

test('keypair private-key workflow retains the full response when extraction fails', async () => {
  const example = await keypairExample(docs[0]);
  const payload = JSON.stringify({keypair: {}});

  await withFakeEcctl(payload, async ({called, root, run}) => {
    const result = run(example);
    assert.notEqual(result.status, 0);
    await stat(called);
    await assertMissing(path.join(root, 'web-key.pem'));

    const names = await readdir(root);
    const responses = names.filter((name) => name.startsWith('ecctl-keypair-response.json.'));
    assert.equal(responses.length, 1);
    assert.equal(await readFile(path.join(root, responses[0]), 'utf8'), payload);
    assert.equal(names.some((name) => name.startsWith('web-key.pem.')), false);
  });
});

test('keypair private-key workflow retains both recovery artifacts on publication race', async () => {
  const example = await keypairExample(docs[0]);
  const privateKey =
    '-----BEGIN PRIVATE KEY-----\nprivate-key-material\n-----END PRIVATE KEY-----\n';
  const payload = JSON.stringify({keypair: {private_key: privateKey}});

  await withFakeEcctl(payload, async ({called, root, run}) => {
    const target = path.join(root, 'web-key.pem');
    const result = run(example, {FAKE_ECCTL_CREATE_TARGET: target});
    assert.notEqual(result.status, 0);
    await stat(called);
    assert.equal(await readFile(target, 'utf8'), 'racer');

    const names = await readdir(root);
    const responses = names.filter((name) => name.startsWith('ecctl-keypair-response.json.'));
    const privateKeys = names.filter((name) => name.startsWith('web-key.pem.'));
    assert.equal(responses.length, 1);
    assert.equal(privateKeys.length, 1);
    assert.equal(await readFile(path.join(root, responses[0]), 'utf8'), payload);
    assert.equal(await readFile(path.join(root, privateKeys[0]), 'utf8'), privateKey);
    assert.equal((await stat(path.join(root, responses[0]))).mode & 0o777, 0o600);
    assert.equal((await stat(path.join(root, privateKeys[0]))).mode & 0o777, 0o600);
  });
});

test('keypair private-key workflow retains the response on ecctl failure', async () => {
  const example = await keypairExample(docs[0]);
  const payload = JSON.stringify({error: {code: 'SyntheticFailure'}});

  await withFakeEcctl(payload, async ({called, root, run}) => {
    const result = run(example, {FAKE_ECCTL_EXIT_CODE: '42'});
    assert.notEqual(result.status, 0);
    await stat(called);
    await assertMissing(path.join(root, 'web-key.pem'));

    const names = await readdir(root);
    const responses = names.filter((name) => name.startsWith('ecctl-keypair-response.json.'));
    assert.equal(responses.length, 1);
    assert.equal(await readFile(path.join(root, responses[0]), 'utf8'), payload);
    assert.equal((await stat(path.join(root, responses[0]))).mode & 0o777, 0o600);
    assert.equal(names.some((name) => name.startsWith('web-key.pem.')), false);
  });
});

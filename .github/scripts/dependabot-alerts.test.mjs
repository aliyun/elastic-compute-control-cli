import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { delimiter, join } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';

function review(t, pages, { exitStatus = 0, repository = '' } = {}) {
  const bin = mkdtempSync(join(tmpdir(), 'ecctl-dependabot-test-'));
  t.after(() => rmSync(bin, { recursive: true, force: true }));
  const argsFile = join(bin, 'args');
  writeFileSync(join(bin, 'gh'), `#!/bin/sh
printf '%s\n' "$@" > "$DEPENDABOT_TEST_ARGS"
printf '%s\n' "$DEPENDABOT_TEST_RESPONSE"
exit "$DEPENDABOT_TEST_STATUS"
`, { mode: 0o700 });

  const result = spawnSync('make', ['--no-print-directory', 'review-dependabot'], {
    cwd: fileURLToPath(new URL('../..', import.meta.url)),
    encoding: 'utf8',
    env: {
      ...process.env,
      PATH: `${bin}${delimiter}${process.env.PATH}`,
      GITHUB_REPOSITORY: repository,
      DEPENDABOT_TEST_ARGS: argsFile,
      DEPENDABOT_TEST_RESPONSE: JSON.stringify(pages),
      DEPENDABOT_TEST_STATUS: String(exitStatus),
    },
  });
  assert.ifError(result.error);
  return { ...result, argsFile };
}

function alert(number, severity, patchedVersion) {
  return {
    number,
    state: 'open',
    dependency: {
      package: { ecosystem: 'npm', name: 'example-package' },
      manifest_path: 'website/package-lock.json',
      relationship: 'transitive',
    },
    security_vulnerability: {
      severity,
      vulnerable_version_range: '< 2.0.0',
      first_patched_version: patchedVersion ? { identifier: patchedVersion } : null,
    },
    security_advisory: { ghsa_id: 'GHSA-xxxx-xxxx-xxxx', classification: 'general' },
    html_url: `https://github.com/aliyun/elastic-compute-control-cli/security/dependabot/${number}`,
  };
}

test('reports all open alerts across pages, sorted by severity', (t) => {
  const result = review(t, [[alert(1, 'low', null)], [alert(2, 'critical', '2.0.0')]]);
  assert.equal(result.status, 0, result.stderr);
  const args = readFileSync(result.argsFile, 'utf8').trimEnd().split('\n');
  assert.ok(args.includes('GET'));
  assert.ok(args.includes('--paginate'));
  assert.ok(args.includes('--slurp'));
  assert.ok(args.includes('repos/aliyun/elastic-compute-control-cli/dependabot/alerts?state=open&per_page=100'));

  const report = JSON.parse(result.stdout);
  assert.equal(report.open, 2);
  assert.deepEqual(report.alerts.map(({ number }) => number), [2, 1]);
  assert.equal(report.alerts[0].manifest, 'website/package-lock.json');
  assert.equal(report.alerts[0].relationship, 'transitive');
  assert.equal(report.alerts[0].scope, 'unknown');
  assert.equal(report.alerts[0].first_patched_version, '2.0.0');
  assert.equal(report.alerts[1].first_patched_version, null);
});

test('reports zero only for a successful, empty alert list', (t) => {
  const result = review(t, [[]], { repository: 'example-org/example.repo' });
  assert.equal(result.status, 0, result.stderr);
  const report = JSON.parse(result.stdout);
  assert.equal(report.repository, 'example-org/example.repo');
  assert.equal(report.open, 0);
  assert.deepEqual(report.alerts, []);
});

test('rejects partial data when the GitHub command fails', (t) => {
  const result = review(t, [[alert(1, 'critical', null)]], { exitStatus: 1 });
  assert.notEqual(result.status, 0);
  assert.equal(result.stdout, '');
  assert.match(result.stderr, /Could not read Dependabot alerts/);
});

test('rejects a response that is not a paginated alert list', (t) => {
  const result = review(t, { message: 'Not Found' });
  assert.notEqual(result.status, 0);
  assert.equal(result.stdout, '');
  assert.match(result.stderr, /expected a paginated alert list/);
});

test('rejects an invalid repository before invoking GitHub CLI', (t) => {
  const result = review(t, [[]], { repository: 'https://github.com/example-org/example.repo' });
  assert.notEqual(result.status, 0);
  assert.equal(result.stdout, '');
  assert.match(result.stderr, /GITHUB_REPOSITORY must be an owner\/repository pair/);
  assert.equal(existsSync(result.argsFile), false);
});

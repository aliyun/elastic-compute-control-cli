import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, realpathSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import { aggregate, branchCounts, createTemporaryDirectories, GOBCO, meetsMinimum, prepareGobco } from './branch-coverage.mjs';

const condition = (overrides = {}) => ({ Start: 'example.go:3:5', Code: 'left && right', TrueCount: 1, FalseCount: 1, ...overrides });

function writeSubprocessTests(write) {
  // Exercise both the generated TestMain and an existing black-box TestMain.
  for (const [directory, packageName] of [['reexec', 'reexec'], ['probe', 'probe_test']]) {
    write(`internal/${directory}/subprocess_test.go`, `package ${packageName}
import (
"os"
"os/exec"
"path/filepath"
"strings"
"testing"
)
func TestScrubbedSubprocessContract(t *testing.T) {
t.Setenv("BRANCH_PARENT_SENTINEL", "must stay in parent")
dir := t.TempDir()
command := func(mode string) *exec.Cmd {
  child := exec.Command(os.Args[0], "-test.run=^TestScrubbedSubprocessHelper$")
  child.Env = []string{"BRANCH_HELPER=" + mode, "BRANCH_HELPER_DIR=" + dir, "PATH=" + os.Getenv("PATH")}
  return child
}
for _, mode := range append([]string{"ok"}, resourceModes()...) {
  if output, err := command(mode).CombinedOutput(); err != nil { t.Fatalf("%s helper: %v\\n%s", mode, err, output) }
  raw, err := os.ReadFile(filepath.Join(dir, mode))
  if err != nil || string(raw) != "helper executed" { t.Fatalf("helper body was not executed for %s: %q, %v", mode, raw, err) }
}
output, err := command("fail").CombinedOutput()
exit, ok := err.(*exec.ExitError)
if !ok || exit.ExitCode() != 1 || !strings.Contains(string(output), "deliberate scrubbed helper failure") {
  t.Fatalf("helper failure was lost or replaced: %v, %s", err, output)
}
}
func TestScrubbedSubprocessHelper(t *testing.T) {
mode := os.Getenv("BRANCH_HELPER")
if mode == "" { return }
if os.Getenv("GOBCO_STATS") != "" || os.Getenv("BRANCH_PARENT_SENTINEL") != "" { t.Fatal("helper environment was not scrubbed") }
if mode == "fail" { t.Fatal("deliberate scrubbed helper failure") }
dir := os.Getenv("BRANCH_HELPER_DIR")
if mode != "ok" { exerciseResourceLimit(t, mode, dir) }
if err := os.WriteFile(filepath.Join(dir, mode), []byte("helper executed"), 0600); err != nil { t.Fatal(err) }
}
`);
    write(`internal/${directory}/resource_unix_test.go`, `//go:build !windows

package ${packageName}
import (
"errors"
"os"
"os/signal"
"path/filepath"
"syscall"
"testing"
)
func resourceModes() []string { return []string{"file-size", "descriptors"} }
func exerciseResourceLimit(t *testing.T, mode, dir string) {
target := filepath.Join(dir, "original-" + mode)
if err := os.WriteFile(target, []byte("original"), 0600); err != nil { t.Fatal(err) }
resource := syscall.RLIMIT_NOFILE
if mode == "file-size" { resource = syscall.RLIMIT_FSIZE }
var before syscall.Rlimit
if err := syscall.Getrlimit(resource, &before); err != nil { t.Fatal(err) }
defer func() { if err := syscall.Setrlimit(resource, &before); err != nil { t.Fatal(err) } }()
var staged *os.File
if mode == "file-size" {
  var err error
  staged, err = os.CreateTemp(dir, "staged-")
  if err != nil { t.Fatal(err) }
  defer staged.Close()
  signal.Ignore(syscall.SIGXFSZ)
  defer signal.Reset(syscall.SIGXFSZ)
}
limited := before
limited.Cur = 0
if err := syscall.Setrlimit(resource, &limited); err != nil { t.Fatal(err) }
var operationErr error
if staged != nil {
  _, operationErr = staged.Write([]byte("replacement"))
} else {
  opened, err := os.OpenFile(target, os.O_WRONLY | os.O_TRUNC, 0600)
  operationErr = err
  if opened != nil { opened.Close() }
}
if err := syscall.Setrlimit(resource, &before); err != nil { t.Fatal(err) }
want := syscall.EMFILE
if mode == "file-size" { want = syscall.EFBIG }
if !errors.Is(operationErr, want) { t.Fatalf("%s operation did not hit the real resource limit: %v", mode, operationErr) }
raw, err := os.ReadFile(target)
if err != nil || string(raw) != "original" { t.Fatalf("resource limit changed target: %q, %v", raw, err) }
}
`);
    write(`internal/${directory}/resource_windows_test.go`, `package ${packageName}
import "testing"
func resourceModes() []string { return nil }
func exerciseResourceLimit(t *testing.T, mode, dir string) { t.Fatal("unexpected Unix resource helper on Windows") }
`);
  }
}

test('branch outcomes count decisions, including an uncovered implicit false outcome', () => {
  assert.deepEqual(branchCounts([condition({ FalseCount: 0 })], ['example.go']), {
    covered: 1, total: 2,
    uncovered: [{ location: 'example.go:3:5', code: 'left && right', outcome: 'false' }],
  });
  assert.equal(branchCounts([condition()], ['example.go']).covered, 2);
  assert.deepEqual(branchCounts([], ['example.go']), { covered: 0, total: 0, uncovered: [] });
});

test('invalid, duplicate, test-file and foreign-file counters fail closed', () => {
  for (const data of [
    null, {}, [null], [condition({ TrueCount: -1 })], [condition({ FalseCount: 0.5 })],
    [condition({ TrueCount: Number.MAX_SAFE_INTEGER + 1 })], [condition({ TrueCount: '1' })],
    [condition({ Start: 'example.go:0:5' })], [condition({ Start: '../example.go:3:5' })],
    [condition({ Start: 'example_test.go:3:5' })], [condition({ Start: 'foreign.go:3:5' })],
    [condition({ Code: '' })], [condition({ Extra: 1 })], [condition(), condition()],
  ]) assert.throws(() => branchCounts(data, ['example.go']), undefined, JSON.stringify(data));
});

test('the floor compares raw counts, including values rounded up to 85%', () => {
  assert.equal(meetsMinimum({ covered: 85, total: 100 }), true);
  assert.equal(meetsMinimum({ covered: 849999, total: 1000000 }), false);
  assert.equal(meetsMinimum({ covered: 86, total: 100 }), true);
  assert.equal(meetsMinimum({ covered: 0, total: 0 }), false);
});

test('aggregation weights outcomes and requires every listed package exactly once', () => {
  const reports = [
    { import_path: 'example/small', covered: 2, total: 2 },
    { import_path: 'example/large', covered: 82, total: 98 },
    { import_path: 'example/empty', covered: 0, total: 0 },
  ];
  assert.deepEqual(aggregate(reports.map(report => report.import_path), reports), { covered: 84, total: 100 });
  assert.equal(meetsMinimum(aggregate(reports.map(report => report.import_path), reports)), false);
  assert.throws(() => aggregate(['example/small', 'example/missing'], reports.slice(0, 1)));
  assert.throws(() => aggregate(['example/small'], [reports[0], reports[0]]));
  assert.throws(() => aggregate(['example/small'], [reports[1]]));
  assert.throws(() => aggregate([], []));
  assert.throws(() => aggregate(['example/empty'], [reports[2]]));
  assert.throws(() => aggregate(['example/small'], [{ ...reports[0], covered: 3 }]));
});

test('temporary workspaces preserve missing-module semantics and clean only owned roots', { timeout: 120000 }, t => {
  const repository = resolve(fileURLToPath(new URL('../..', import.meta.url)));
  mkdirSync(join(repository, 'reports'), { recursive: true });
  const output = mkdtempSync(join(repository, 'reports', 'branch-temp-test-'));
  const external = mkdtempSync(join(tmpdir(), 'branch-temp-base-'));
  const originalEnvironment = Object.fromEntries(['TMPDIR', 'TMP', 'TEMP'].map(key => [key, process.env[key]]));
  let directories;
  try {
    writeFileSync(join(output, 'go.mod'), 'module trap\n');
    writeFileSync(join(external, 'unrelated'), 'keep');
    assert.throws(() => createTemporaryDirectories(output, output), /go\.mod/);
    assert.deepEqual(readdirSync(output), ['go.mod'], 'reject module ancestry before allocating directories');
    directories = createTemporaryDirectories(output, external);
    assert.equal(dirname(realpathSync(directories.work)), realpathSync(output));
    assert.equal(dirname(realpathSync(directories.tests)), realpathSync(external));
    assert.deepEqual(directories.environment, { TMPDIR: directories.tests, TMP: directories.tests, TEMP: directories.tests });
    assert.equal(statSync(directories.tests).mode & 0o777, 0o700);
    for (let path = realpathSync(directories.tests); ; path = dirname(path)) {
      if (existsSync(join(path, 'go.mod'))) assert.ok(statSync(join(path, 'go.mod')).isDirectory(), `test root inherits module ${path}`);
      if (dirname(path) === path) break;
    }
    const result = spawnSync('go', ['test', './pkg/spec', '-run', '^TestDefaultSpecDirectoryFindsModuleAncestor$', '-count=1', '-v'], {
      cwd: repository, encoding: 'utf8', timeout: 90000,
      env: { ...process.env, ...directories.environment },
    });
    assert.equal(result.status, 0, `${result.error ?? ''}\n${result.stdout}\n${result.stderr}`);
    directories.cleanup();
    assert.equal(existsSync(directories.work), false);
    assert.equal(existsSync(directories.tests), false);
    assert.equal(readFileSync(join(external, 'unrelated'), 'utf8'), 'keep');
    assert.equal(readFileSync(join(output, 'go.mod'), 'utf8'), 'module trap\n');
    assert.deepEqual(Object.fromEntries(['TMPDIR', 'TMP', 'TEMP'].map(key => [key, process.env[key]])), originalEnvironment);
    t.diagnostic(JSON.stringify({ temporary_root_contract_only: true, unchanged_DefaultDir_test: 'passed',
      module_temporary_base: 'rejected', test_root_module_ancestor: null, owned_cleanup: 'passed', unrelated_parents_preserved: true }));
    directories = undefined;
  } finally {
    directories?.cleanup();
    rmSync(output, { recursive: true, force: true });
    rmSync(external, { recursive: true, force: true });
  }
});

test('runtime templates preserve scrubbed helpers and real resource limits', { timeout: 120000 }, t => {
  const repository = resolve(fileURLToPath(new URL('../..', import.meta.url)));
  mkdirSync(join(repository, 'reports'), { recursive: true });
  const fixture = mkdtempSync(join(repository, 'reports', 'branch-runtime-test-'));
  function write(path, content) {
    mkdirSync(resolve(fixture, path, '..'), { recursive: true });
    writeFileSync(join(fixture, path), content);
  }
  try {
    const download = spawnSync('go', ['mod', 'download', '-json', GOBCO], { encoding: 'utf8', timeout: 60000 });
    assert.equal(download.status, 0, download.stderr);
    const module = JSON.parse(download.stdout);
    assert.equal(module.Sum, 'h1:I2qs2i0RvEKCS//lh8uCluFkXgSE/F2yjRXqC9IaUSE=');
    const prepared = prepareGobco(module, fixture);
    write('go.mod', 'module example.invalid/runtimefixture\n\ngo 1.25.8\n');
    writeSubprocessTests(write);
    for (const name of ['probe', 'reexec']) {
      const template = path => readFileSync(join(prepared.source, 'templates', path), 'utf8')
        .replace('//go:build ignore\n// +build ignore\n\n', '').replace('package main\n', `package ${name}\n`);
      write(`internal/${name}/gobco_fixed.go`, template('gobco_fixed.go'));
      write(`internal/${name}/gobco_variable.go`, `package ${name}\nvar gobcoOpts gobcoOptions\nvar gobcoCounts = gobcoStats{conds: []gobcoCond{}}\n`);
      if (name === 'reexec') write(`internal/${name}/gobco_no_testmain_test.go`, template('gobco_no_testmain_test.go'));
      else write(`internal/${name}/main_test.go`, `package probe_test\nimport ("os"; "testing"; "example.invalid/runtimefixture/internal/probe")\nfunc TestMain(m *testing.M) { os.Exit(probe.GobcoFinish(m.Run())) }\n`);
    }
    const stats = join(fixture, 'runtime-stats.json');
    const result = spawnSync('go', ['test', '-p=1', '-count=1', '-v', './...'], {
      cwd: fixture, env: { ...process.env, GOBCO_STATS: stats }, encoding: 'utf8', timeout: 90000,
    });
    assert.equal(result.status, 0, `${result.error ?? ''}\n${result.stdout}\n${result.stderr}`);
    assert.deepEqual(JSON.parse(readFileSync(stats, 'utf8')), [], 'the parent must persist its real runtime data');
    t.diagnostic(JSON.stringify({ runtime_contract_only: true, patch: prepared.runtime_patch,
      generated_TestMain: 'passed', existing_TestMain: 'passed', scrubbed_helpers: 'passed',
      unix_resource_limits: process.platform === 'win32' ? 'not_exercised' : 'passed', helper_exit_status: 'passed' }));
  } finally {
    rmSync(fixture, { recursive: true, force: true });
  }
});

test('the pinned tool measures real decisions, retains untested packages and propagates failures', { timeout: 240000 }, t => {
  assert.equal(GOBCO, 'github.com/rillig/gobco@v1.3.3');
  const repository = resolve(fileURLToPath(new URL('../..', import.meta.url)));
  mkdirSync(join(repository, 'reports'), { recursive: true });
  const fixture = mkdtempSync(join(repository, 'reports', 'branch-tool-test-'));
  const collector = fileURLToPath(new URL('./branch-coverage.mjs', import.meta.url));
  function command(executable, args) {
    return spawnSync(executable, args, { cwd: fixture, env: process.env, encoding: 'utf8', timeout: 180000 });
  }
  function succeeds(executable, args) {
    const result = command(executable, args);
    const diagnostics = result.status === 0 ? '' : ['coverage.json', 'tool-build.log', 'tool-version.log']
      .map(name => join(fixture, 'reports', 'branch-coverage', name)).filter(existsSync)
      .map(path => `${path}\n${readFileSync(path, 'utf8')}`).join('\n');
    assert.equal(result.status, 0, `${executable}: ${result.error ?? ''}\n${result.stdout}\n${result.stderr}\n${diagnostics}`);
    return result;
  }
  function write(path, content) {
    mkdirSync(resolve(fixture, path, '..'), { recursive: true });
    writeFileSync(join(fixture, path), content);
  }
  function report() {
    return JSON.parse(readFileSync(join(fixture, 'reports', 'branch-coverage', 'coverage.json'), 'utf8'));
  }
  try {
    write('go.mod', 'module example.invalid/branchfixture\n\ngo 1.25.8\n');
    write('internal/probe/probe.go', `package probe
type Flag bool
func touch(value bool, calls *int) bool { *calls++; return value }
func Decide(left, right bool, calls *int) bool {
  if left && touch(right, calls) { return true }; return false
}
func Flagged(value Flag) bool { if value { return true }; return false }
func Kind(value any) string {
  switch item := value.(type) {
  case int: if item > 0 { return "positive" }; return "zero"
  case string: return item
  default: return "other"
  }
}
func Loop(n int) int { for n > 0 { n-- }; return n }
func Implicit(value bool) int { if value { return 1 }; return 0 }
`);
    write('internal/probe/probe_test.go', `package probe_test
import (
  "os"
  "testing"
  "example.invalid/branchfixture/internal/probe"
)
func TestMain(m *testing.M) { os.Exit(m.Run()) }
func TestDecisions(t *testing.T) {
  for _, item := range []struct { left, right, want bool; calls int }{
    {false, true, false, 0}, {true, false, false, 1}, {true, true, true, 1},
  } {
    calls := 0
    if got := probe.Decide(item.left, item.right, &calls); got != item.want || calls != item.calls { t.Fatalf("got %v, calls %d", got, calls) }
  }
  if !probe.Flagged(true) || probe.Flagged(false) { t.Fatal("named bool") }
  if probe.Kind(1) != "positive" || probe.Kind(0) != "zero" || probe.Kind("text") != "text" || probe.Kind(nil) != "other" { t.Fatal("type switch") }
  if probe.Loop(2) != 0 || probe.Loop(0) != 0 { t.Fatal("loop") }
  if probe.Implicit(true) != 1 { t.Fatal("implicit else") }
}
`);
    write('internal/untested/branch.go', 'package untested\nfunc Sign(n int) int { if n > 0 { return 1 }; return 0 }\n');
    write('internal/reexec/empty.go', 'package reexec\n');
    writeSubprocessTests(write);
    write('internal/probe/temporary_test.go', `package probe_test
import ("os"; "path/filepath"; "testing")
func TestTemporaryDirectoryHasNoModuleAncestor(t *testing.T) {
  for path := t.TempDir(); ; path = filepath.Dir(path) {
    if info, err := os.Stat(filepath.Join(path, "go.mod")); err == nil && !info.IsDir() { t.Fatalf("temporary root inherits module %s", path) } else if err != nil && !os.IsNotExist(err) { t.Fatal(err) }
    if filepath.Dir(path) == path { break }
  }
}
`);
    write('cmd/specgen/main.go', 'package main\nfunc main() { if true { panic("generator must stay outside the existing metric") } }\n');
    write('.gitignore', '/reports/\n');
    succeeds('git', ['init', '--quiet']);
    succeeds('git', ['add', '.']);
    succeeds('git', ['-c', 'user.name=Coverage fixture', '-c', 'user.email=coverage@example.invalid', '-c', 'core.hooksPath=/dev/null', 'commit', '--quiet', '-m', 'fixture']);

    succeeds(process.execPath, [collector, '--report-only', './...']);
    const measured = report();
    assert.equal(measured.complete, true);
    assert.equal(measured.threshold_met, false);
    assert.equal(measured.tool.module, GOBCO);
    assert.equal(measured.temporary_directories.policy, 'module-independent-test-root-v1');
    assert.equal(measured.temporary_directories.cleaned, true);
    assert.equal(existsSync(measured.temporary_directories.work), false);
    assert.equal(existsSync(measured.temporary_directories.tests), false);
    assert.equal(measured.packages.length, 3);
    assert.deepEqual({ covered: measured.covered, total: measured.total }, { covered: 13, total: 16 }, 'runtime compatibility must preserve the decision inventory and hits');
    assert.equal(measured.tool.runtime_patch.id, 'ecctl-gobco-subprocess-v1');
    assert.equal(measured.tool.runtime_patch.templates.length, 2);
    for (const template of measured.tool.runtime_patch.templates) {
      assert.match(template.before_sha256, /^[a-f0-9]{64}$/);
      assert.match(template.after_sha256, /^[a-f0-9]{64}$/);
      assert.notEqual(template.before_sha256, template.after_sha256);
    }
    const probe = measured.packages.find(pkg => pkg.import_path.endsWith('/probe'));
    const untested = measured.packages.find(pkg => pkg.import_path.endsWith('/untested'));
    assert.deepEqual({ covered: untested.covered, total: untested.total }, { covered: 0, total: 2 });
    assert.ok(probe.uncovered.some(item => item.code === 'value' && item.outcome === 'false'));
    const raw = JSON.parse(readFileSync(join(fixture, 'reports', 'branch-coverage', probe.profile), 'utf8'));
    const compound = raw.filter(item => item.Code === 'left && touch(right, calls)');
    assert.equal(compound.length, 1, 'one whole controlling decision, not operand conditions');
    assert.ok(compound[0].TrueCount > 0 && compound[0].FalseCount > 0);
    assert.ok(raw.every(item => !item.Start.includes('_test.go:')));
    t.diagnostic(JSON.stringify({ fixture_only: true, tool: measured.tool, platform: measured.platform,
      covered: measured.covered, total: measured.total, threshold_met: measured.threshold_met,
      packages: measured.packages.map(pkg => ({ import_path: pkg.import_path, covered: pkg.covered, total: pkg.total })),
      compound_decision: compound[0] }));
    const source = readFileSync(join(fixture, 'internal/probe/probe.go'), 'utf8');
    assert.ok(!source.includes('GobcoCover'), 'source files must remain uninstrumented');

    const rejected = command(process.execPath, [collector, './...']);
    assert.equal(rejected.status, 1, rejected.stdout + rejected.stderr);
    assert.equal(report().complete, true);
    assert.equal(report().threshold_met, false);

    write('internal/untested/branch_test.go', 'package untested\nimport "testing"\nfunc TestSign(t *testing.T) { if Sign(1) != 1 || Sign(0) != 0 { t.Fatal("sign") } }\n');
    write('internal/probe/probe_test.go', readFileSync(join(fixture, 'internal/probe/probe_test.go'), 'utf8') + '\nfunc TestOtherOutcome(t *testing.T) { if probe.Implicit(false) != 0 { t.Fatal("false outcome") } }\n');
    succeeds(process.execPath, [collector, './...']);
    assert.equal(report().complete, true);
    assert.equal(report().threshold_met, true, 'asserting the missing behaviors must satisfy the normal gate');

    write('internal/probe/probe_test.go', readFileSync(join(fixture, 'internal/probe/probe_test.go'), 'utf8') + '\nfunc TestFailure(t *testing.T) { t.Fatal("intentional failure") }\n');
    const failed = command(process.execPath, [collector, '--report-only', './...']);
    assert.equal(failed.status, 1, failed.stdout + failed.stderr);
    const failedReport = report();
    assert.equal(failedReport.complete, false, 'stale successful reports cannot survive test failure');
    assert.equal(failedReport.threshold_met, false);
    assert.equal(failedReport.temporary_directories.cleaned, true);
    assert.equal(existsSync(failedReport.temporary_directories.work), false);
    assert.equal(existsSync(failedReport.temporary_directories.tests), false);
    assert.match(failedReport.error, /probe|go test|gobco/);
  } finally {
    rmSync(fixture, { recursive: true, force: true });
  }
});

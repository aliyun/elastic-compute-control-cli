import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { chmodSync, copyFileSync, cpSync, existsSync, lstatSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, isAbsolute, join, relative, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

export const GOBCO = 'github.com/rillig/gobco@v1.3.3';
const toolSum = 'h1:I2qs2i0RvEKCS//lh8uCluFkXgSE/F2yjRXqC9IaUSE=';
const minimum = 85;
const defaultPackages = ['./internal/...', './pkg/...', './cmd/...', './specs/...'];

// Adapt only the injected test entrypoints. A re-executed test binary may
// intentionally omit coverage variables; it must still run and return errors.
export function prepareGobco(downloaded, work) {
  const source = join(work, 'gobco-source');
  cpSync(downloaded.Dir, source, { recursive: true });
  const patches = [
    ['templates/gobco_no_testmain_test.go',
      'func TestMain(m *testing.M) {\n',
      'func TestMain(m *testing.M) {\n\tif os.Getenv("GOBCO_STATS") == "" {\n\t\tos.Exit(m.Run())\n\t}\n'],
    ['templates/gobco_fixed.go',
      'func (st *gobcoStats) finish(exitCode int) int {\n',
      'func (st *gobcoStats) finish(exitCode int) int {\n\tif os.Getenv("GOBCO_STATS") == "" {\n\t\treturn exitCode\n\t}\n'],
  ];
  const hash = value => createHash('sha256').update(value).digest('hex');
  const templates = patches.map(([path, before, after]) => {
    const target = join(source, path);
    const original = readFileSync(target, 'utf8');
    if (original.split(before).length !== 2) throw new Error(`Pinned gobco template does not match runtime patch: ${path}`);
    const patched = original.replace(before, after);
    chmodSync(target, 0o600); // Module-cache copies retain their read-only mode.
    writeFileSync(target, patched);
    return { path, before_sha256: hash(original), after_sha256: hash(patched) };
  });
  return { source, runtime_patch: { id: 'ecctl-gobco-subprocess-v1', templates } };
}

export function buildGobco(downloaded, work, env, output) {
  const prepared = prepareGobco(downloaded, work);
  const executable = join(work, process.platform === 'win32' ? 'gobco.exe' : 'gobco');
  run('go', ['build', '-mod=readonly', '-buildvcs=false', '-o', executable, '.'], prepared.source, env, join(output, 'tool-build.log'));
  return { executable, runtime_patch: prepared.runtime_patch };
}

export function branchCounts(conditions, goFiles) {
  if (!Array.isArray(conditions)) throw new Error('Expected a gobco outcome array');
  const files = new Set(goFiles);
  const seen = new Set();
  const uncovered = [];
  let covered = 0;
  for (const condition of conditions) {
    if (!condition || Object.keys(condition).sort().join(',') !== 'Code,FalseCount,Start,TrueCount' ||
        typeof condition.Code !== 'string' || !condition.Code.trim() || typeof condition.Start !== 'string') {
      throw new Error('Invalid gobco outcome record');
    }
    const match = /^([^/\\:]+\.go):[1-9]\d*:[1-9]\d*$/.exec(condition.Start);
    if (!match || match[1].endsWith('_test.go') || !files.has(match[1])) {
      throw new Error(`Outcome is outside compiled production files: ${condition.Start}`);
    }
    const key = JSON.stringify([condition.Start, condition.Code]);
    if (seen.has(key)) throw new Error(`Duplicate decision: ${condition.Start}`);
    seen.add(key);
    for (const [field, outcome] of [['TrueCount', 'true'], ['FalseCount', 'false']]) {
      const count = condition[field];
      if (!Number.isSafeInteger(count) || count < 0) throw new Error(`Invalid gobco ${field}`);
      if (count > 0) covered++;
      else uncovered.push({ location: condition.Start, code: condition.Code, outcome });
    }
  }
  return { covered, total: conditions.length * 2, uncovered };
}

export function meetsMinimum({ covered, total }) {
  return total > 0 && BigInt(covered) * 100n >= BigInt(total) * BigInt(minimum);
}

export function aggregate(expectedPackages, reports) {
  const remaining = new Set(expectedPackages);
  if (!remaining.size || remaining.size !== expectedPackages.length) throw new Error('Invalid package inventory');
  let covered = 0;
  let total = 0;
  for (const report of reports) {
    if (!remaining.delete(report.import_path)) throw new Error(`Unexpected or duplicate package: ${report.import_path}`);
    if (!Number.isSafeInteger(report.covered) || !Number.isSafeInteger(report.total) ||
        report.covered < 0 || report.covered > report.total || report.total < 0 || report.total % 2) {
      throw new Error(`Invalid package totals: ${report.import_path}`);
    }
    covered += report.covered;
    total += report.total;
  }
  if (remaining.size) throw new Error(`Missing package reports: ${[...remaining].join(', ')}`);
  if (!Number.isSafeInteger(total) || !total) throw new Error('Empty or invalid aggregate branch data');
  return { covered, total };
}

function run(executable, args, cwd, env, logPath) {
  const result = spawnSync(executable, args, { cwd, env, encoding: 'utf8', timeout: 720000, maxBuffer: 64 * 1024 * 1024 });
  if (logPath) writeFileSync(logPath, `${JSON.stringify({ argv: [executable, ...args], cwd, exit_code: result.status, signal: result.signal })}\n${result.stdout ?? ''}${result.stderr ?? ''}`, { mode: 0o600 });
  if (result.error || result.status !== 0) throw new Error(`${executable} ${args.join(' ')} failed (exit ${result.status}, signal ${result.signal}): ${result.error?.message ?? result.stderr?.trim() ?? result.status}`);
  return result.stdout;
}

function directory(path) {
  if (existsSync(path)) {
    const info = lstatSync(path);
    if (!info.isDirectory() || info.isSymbolicLink()) throw new Error(`Unsafe report directory: ${path}`);
  } else mkdirSync(path, { recursive: true, mode: 0o700 });
}

function inside(root, path) {
  const part = relative(root, path);
  if (!part || part === '..' || part.startsWith(`..${sep}`) || isAbsolute(part)) throw new Error(`Path is outside source root: ${path}`);
  return part;
}

export function createTemporaryDirectories(output, temporaryBase = tmpdir()) {
  const base = realpathSync(temporaryBase);
  for (let path = base; ; path = dirname(path)) {
    let module;
    try { module = statSync(join(path, 'go.mod')); }
    catch (error) { if (error.code !== 'ENOENT') throw error; }
    if (module && !module.isDirectory()) throw new Error(`Test temporary base has a go.mod ancestor: ${join(path, 'go.mod')}`);
    if (dirname(path) === path) break;
  }
  const owned = [];
  const cleanup = () => {
    const errors = [];
    for (const path of [...owned].reverse()) {
      try { rmSync(path, { recursive: true, force: true }); }
      catch (error) { errors.push(error); }
    }
    if (errors.length) throw new AggregateError(errors, `Temporary-directory cleanup failed: ${errors.map(error => error.message).join('; ')}`);
  };
  try {
    const work = mkdtempSync(join(output, 'work-'));
    owned.push(work);
    const tests = mkdtempSync(join(base, 'ecctl-branch-tests-'));
    owned.push(tests);
    return { work, tests, environment: { TMPDIR: tests, TMP: tests, TEMP: tests }, cleanup };
  } catch (error) {
    try { cleanup(); }
    catch (cleanupError) { throw new Error(`${error.message}; ${cleanupError.message}`); }
    throw error;
  }
}

// Gobco copies an entire module for each package. Supply source and prepared
// metadata, without recursively copying reports, tool caches or node_modules.
function snapshot(root, destination, env) {
  const paths = run('git', ['ls-files', '--cached', '--others', '--exclude-standard', '-z'], root, env).split('\0').filter(Boolean);
  for (const path of new Set(paths)) {
    const source = resolve(root, path);
    inside(root, source);
    if (!existsSync(source)) continue; // A tracked deletion in a local report.
    const info = lstatSync(source);
    if (!info.isFile() || info.isSymbolicLink()) throw new Error(`Source is not a regular file: ${path}`);
    const target = join(destination, path);
    mkdirSync(dirname(target), { recursive: true });
    copyFileSync(source, target);
  }
  const metadata = join(root, 'internal/openapimeta/.generated');
  if (existsSync(metadata)) cpSync(metadata, join(destination, 'internal/openapimeta/.generated'), { recursive: true });
}

function summary(report) {
  const lines = ['# Branch coverage', '', `Tool: ${GOBCO} with explicit \`-branch\`; runtime patch: ${report.tool.runtime_patch?.id ?? 'not built'}.`, '',
    `Source: ${report.source?.head_sha ?? 'unknown'}; platform: ${report.platform ?? 'unknown'}.`, '',
    report.complete ? `Total: ${report.covered}/${report.total} (${report.percentage}%); required: ${minimum}%; threshold met: ${report.threshold_met}.` : `Collection incomplete; gate failed. ${report.error ?? ''}`, '',
    '| Package | Covered outcomes | Total outcomes |', '| --- | ---: | ---: |'];
  for (const pkg of report.packages) lines.push(`| ${pkg.import_path} | ${pkg.covered} | ${pkg.total} |`);
  lines.push('', '## Uncovered outcomes', '');
  for (const pkg of report.packages) {
    for (const item of pkg.uncovered) lines.push(`- ${pkg.directory}/${item.location}: ${item.outcome} for ${JSON.stringify(item.code)}`);
  }
  return lines.join('\n') + '\n';
}

export function main(args) {
  const reportOnly = args[0] === '--report-only';
  const patterns = reportOnly ? args.slice(1) : args;
  if (patterns.some(arg => arg.startsWith('-'))) throw new Error('usage: branch-coverage.mjs [--report-only] [package patterns]');
  const root = process.cwd();
  directory(join(root, 'reports'));
  const output = join(root, 'reports/branch-coverage');
  directory(output);
  rmSync(output, { recursive: true });
  directory(output);
  let temporary;
  const report = {
    schema_version: 1, metric: 'gobco-branch-outcomes-v1', tool: { module: GOBCO, sum: toolSum },
    minimum, report_only: reportOnly, complete: false, threshold_met: false,
    package_patterns: patterns.length ? patterns : defaultPackages,
    excluded_packages: ['cmd/specgen'], expected_packages: [], packages: [],
  };
  try {
    temporary = createTemporaryDirectories(output);
    const work = temporary.work;
    report.temporary_directories = { work, tests: temporary.tests, policy: 'module-independent-test-root-v1', cleaned: false };
    const initialEnv = { ...process.env, GOWORK: 'off', GOFLAGS: '' };
    const host = JSON.parse(run('go', ['env', '-json', 'GOHOSTOS', 'GOHOSTARCH', 'GOVERSION'], root, initialEnv));
    report.platform = `${host.GOHOSTOS}/${host.GOHOSTARCH}`;
    report.go_version = host.GOVERSION;
    const env = { ...initialEnv, GOOS: host.GOHOSTOS, GOARCH: host.GOHOSTARCH, ...temporary.environment };
    report.source = {
      head_sha: run('git', ['rev-parse', '--verify', 'HEAD'], root, env).trim(),
      status: run('git', ['status', '--short', '-uall'], root, env).trim(),
    };
    const downloaded = JSON.parse(run('go', ['mod', 'download', '-json', GOBCO], root, env, join(output, 'tool.log')));
    if (downloaded.Sum !== toolSum || downloaded.Version !== 'v1.3.3') throw new Error('Pinned gobco module checksum/version mismatch');
    const tool = buildGobco(downloaded, work, env, output);
    report.tool.runtime_patch = tool.runtime_patch;
    const version = run(tool.executable, ['-version'], root, env, join(output, 'tool-version.log')).trim();
    if (version !== '1.3.3') throw new Error(`Unexpected gobco version: ${version}`);
    report.tool.version = version;
    report.tool.commit = downloaded.Origin?.Hash ?? null;
    const listed = run('go', ['list', '-json=Dir,ImportPath,GoFiles,CgoFiles', ...report.package_patterns], root, env);
    // go list emits successive, indented top-level JSON objects.
    const packages = JSON.parse(`[${listed.trim().replace(/^}\r?\n{/gm, '},{')}]`).map(pkg => ({
      import_path: pkg.ImportPath, directory: inside(root, pkg.Dir).split(sep).join('/'),
      source_files: [...(pkg.GoFiles ?? []), ...(pkg.CgoFiles ?? [])],
    })).filter(pkg => pkg.directory !== 'cmd/specgen');
    report.expected_packages = packages.map(pkg => pkg.import_path);
    if (!packages.length) throw new Error('No packages found in the branch metric');
    const source = join(work, 'source');
    directory(source);
    snapshot(root, source, env);
    for (const pkg of packages) {
      const profile = `packages/${pkg.directory}.json`;
      const log = `packages/${pkg.directory}.log`;
      mkdirSync(dirname(join(output, profile)), { recursive: true });
      console.log(`Branch coverage: ${pkg.import_path}`);
      let stdout;
      try {
        stdout = run(tool.executable, ['-branch', '-stats', join(output, profile), '-test=-timeout=10m', '.'], join(source, pkg.directory), env, join(output, log));
      } catch (error) {
        throw new Error(`${pkg.import_path}: ${error.message}; see ${log}`);
      }
      const counts = branchCounts(JSON.parse(readFileSync(join(output, profile), 'utf8')), pkg.source_files);
      const totals = [...stdout.matchAll(/^Branch coverage: (\d+)\/(\d+)\r?$/gm)];
      if (totals.length !== 1 || Number(totals[0][1]) !== counts.covered || Number(totals[0][2]) !== counts.total) {
        throw new Error(`${pkg.import_path}: gobco summary/profile mismatch; see ${log}`);
      }
      report.packages.push({ ...pkg, ...counts, profile, log });
    }
    Object.assign(report, aggregate(report.expected_packages, report.packages));
    report.percentage = (report.covered * 100 / report.total).toFixed(6);
    report.threshold_met = meetsMinimum(report);
    report.complete = true;
  } catch (error) {
    report.error = error.message;
    console.error(error.message);
  } finally {
    if (temporary) {
      try { temporary.cleanup(); report.temporary_directories.cleaned = true; }
      catch (error) {
        report.complete = false;
        report.threshold_met = false;
        report.error = [report.error, error.message].filter(Boolean).join('; ');
        console.error(error.message);
      }
    }
    writeFileSync(join(output, 'coverage.json'), JSON.stringify(report, null, 2) + '\n', { mode: 0o600 });
    writeFileSync(join(output, 'summary.md'), summary(report), { mode: 0o600 });
  }
  if (report.complete) console.log(`Total branch coverage: ${report.percentage}% (${report.covered}/${report.total}); required: ${minimum}%${reportOnly ? ' (report only)' : ''}`);
  console.log(`Branch report: ${join(output, 'summary.md')}`);
  return report.complete && (reportOnly || report.threshold_met) ? 0 : 1;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { process.exitCode = main(process.argv.slice(2)); }
  catch (error) { console.error(error.message); process.exitCode = 1; }
}

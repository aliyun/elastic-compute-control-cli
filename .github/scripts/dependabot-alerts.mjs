import { execFileSync } from 'node:child_process';

const repository = process.env.GITHUB_REPOSITORY || 'aliyun/elastic-compute-control-cli';
const severityOrder = new Map(['critical', 'high', 'medium', 'low'].map((severity, index) => [severity, index]));

try {
  if (!/^[\w-]+\/[\w.-]+$/.test(repository) || repository.endsWith('/.') || repository.endsWith('/..')) {
    throw new Error('GITHUB_REPOSITORY must be an owner/repository pair');
  }

  const response = execFileSync('gh', [
    'api', '--hostname', 'github.com', '--method', 'GET', '--paginate', '--slurp',
    '-H', 'X-GitHub-Api-Version: 2022-11-28',
    `repos/${repository}/dependabot/alerts?state=open&per_page=100`,
  ], {
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'inherit'],
    maxBuffer: 32 * 1024 * 1024,
  });

  const pages = JSON.parse(response);
  if (!Array.isArray(pages) || pages.length === 0 || !pages.every(Array.isArray)) {
    throw new Error('expected a paginated alert list');
  }

  const alerts = pages.flat().map((alert) => {
    if (alert.state !== 'open' || !Number.isInteger(alert.number)) {
      throw new Error('expected an open alert with a repository alert number');
    }
    const { dependency, security_advisory: advisory, security_vulnerability: vulnerability } = alert;
    return {
      number: alert.number,
      severity: vulnerability.severity ?? advisory.severity ?? 'unknown',
      classification: advisory.classification ?? 'unknown',
      ecosystem: dependency.package.ecosystem,
      package: dependency.package.name,
      manifest: dependency.manifest_path,
      scope: dependency.scope ?? 'unknown',
      relationship: dependency.relationship ?? 'unknown',
      vulnerable_versions: vulnerability.vulnerable_version_range,
      first_patched_version: vulnerability.first_patched_version?.identifier ?? null,
      ghsa: advisory.ghsa_id,
      cve: advisory.cve_id ?? null,
      url: alert.html_url,
    };
  });

  alerts.sort((left, right) =>
    (severityOrder.get(left.severity) ?? severityOrder.size) -
    (severityOrder.get(right.severity) ?? severityOrder.size) || left.number - right.number);
  console.log(JSON.stringify({ repository, open: alerts.length, alerts }, null, 2));
} catch (error) {
  console.error(`Could not read Dependabot alerts: ${error.message}`);
  process.exitCode = 1;
}

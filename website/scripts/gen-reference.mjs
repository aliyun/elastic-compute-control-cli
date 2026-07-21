// Generates per-resource reference pages from `ecctl schema`.
//
// schema is the source of truth for the command surface, so these pages are
// generated rather than hand-written. Re-run after changing resource specs:
//
//   make build            # refresh ./bin/ecctl
//   npm --prefix website run gen:reference
//
// Override the binary with ECCTL_BIN. The generated Markdown is committed.

import {execFileSync} from 'node:child_process';
import {mkdirSync, writeFileSync, readFileSync, rmSync} from 'node:fs';
import {dirname, join} from 'node:path';
import {fileURLToPath} from 'node:url';

import {apiTable, escCell} from './gen-reference-lib.mjs';
import {
  resourceReferenceFilename,
  resourceReferenceLabel,
} from './reference-path.mjs';

const SCRIPT_DIR = dirname(fileURLToPath(import.meta.url));
const WEBSITE_DIR = join(SCRIPT_DIR, '..');
const ECCTL = process.env.ECCTL_BIN || join(WEBSITE_DIR, '..', 'bin', 'ecctl');

// Contract / escape flags shared by every command. They are summarized in the
// contract lines and documented in the User Guide, so they are omitted from the
// per-command parameter tables to keep them focused on real inputs.
const COMMON_FLAGS = new Set(['api-param', 'idempotency-key', 'no-wait', 'timeout', 'dry-run']);

// Canonical action ordering; anything else keeps its schema order afterwards.
const ACTION_ORDER = ['create', 'update', 'delete', 'get', 'list'];

const LOCALES = [
  {
    lang: 'en',
    dir: join(WEBSITE_DIR, 'docs', 'reference', 'resources'),
    t: {
      intro: (cmd, dotted) =>
        `Run \`ecctl ${cmd} <action> -h\` for usage, or \`ecctl schema ${dotted}.<action> --full\` for the complete, agent-readable spec of every parameter and behavior.`,
      paramHeader: '| Parameter | Type | Required | Description |\n|---|---|---|---|',
      kindRisk: (kind, risk) => `- Kind: \`${kind}\` · Risk: ${risk}`,
      wait: (w) =>
        `- Synchronous: waits for \`${w.target}\` (waiter \`${w.name}\`, timeout \`${w.timeout}\`); use \`--no-wait\` to skip.`,
      idem: (field) => `- Idempotent via \`${field}\`.`,
      dryRun: '- Dry run: supported via `--dry-run`.',
      apiHeader: '| API | When called | Purpose |\n|---|---|---|',
      repeated: 'repeated',
      cached: 'cached; no additional request',
      annotate: (purpose, note) => `${purpose} (${note})`,
      defaultLabel: (v) => ` (default: \`${v}\`)`,
      indexTitle: (code) => `${code} resources`,
    },
  },
  {
    lang: 'zh-CN',
    dir: join(
      WEBSITE_DIR,
      'i18n',
      'zh-Hans',
      'docusaurus-plugin-content-docs',
      'current',
      'reference',
      'resources',
    ),
    t: {
      intro: (cmd, dotted) =>
        `运行 \`ecctl ${cmd} <action> -h\` 查看用法，或 \`ecctl schema ${dotted}.<action> --full\` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。`,
      paramHeader: '| 参数 | 类型 | 必填 | 说明 |\n|---|---|---|---|',
      kindRisk: (kind, risk) => `- 类型：\`${kind}\` · 风险：${risk}`,
      wait: (w) =>
        `- 同步：等待 \`${w.target}\`（waiter \`${w.name}\`，超时 \`${w.timeout}\`）；用 \`--no-wait\` 跳过等待。`,
      idem: (field) => `- 通过 \`${field}\` 幂等。`,
      dryRun: '- 支持 `--dry-run` 校验。',
      apiHeader: '| API | 调用时机 | 用途 |\n|---|---|---|',
      repeated: '重复调用',
      cached: '复用等待结果，不额外请求',
      annotate: (purpose, note) => `${purpose}（${note}）`,
      defaultLabel: (v) => `（默认：\`${v}\`）`,
      indexTitle: (code) => `${code} 资源`,
    },
  },
];

function schema(args, lang) {
  const out = execFileSync(ECCTL, ['--lang', lang, '--output', 'json', 'schema', ...args], {
    encoding: 'utf8',
    maxBuffer: 32 * 1024 * 1024,
  });
  return JSON.parse(out);
}

function orderedActions(actions) {
  const known = ACTION_ORDER.filter((a) => actions.includes(a));
  const rest = actions.filter((a) => !ACTION_ORDER.includes(a));
  return [...known, ...rest];
}

function usageLine(action) {
  let line = action.cli;
  for (const p of action.positionals || []) {
    line += p.many ? ` [<${p.name}>...]` : ` <${p.name}>`;
  }
  return `${line} [flags]`;
}

function paramRows(action, t) {
  const positionals = new Set((action.positionals || []).map((p) => p.name));
  const rows = Object.entries(action.params || {})
    .filter(
      ([name, p]) =>
        !COMMON_FLAGS.has(name) && !positionals.has(name) && !p.positional_many,
    )
    .map(([name, p]) => ({name, ...p}))
    .sort(
      (a, b) =>
        (b.required ? 1 : 0) - (a.required ? 1 : 0) || a.name.localeCompare(b.name),
    );
  if (!rows.length) return '';
  const body = rows
    .map((p) => {
      const def = p.default !== undefined ? t.defaultLabel(p.default) : '';
      return `| \`--${p.name}\` | ${p.type || ''} | ${p.required ? '✓' : ''} | ${escCell(p.description)}${def} |`;
    })
    .join('\n');
  return `${t.paramHeader}\n${body}`;
}

function actionSection(action, t) {
  const blocks = [`## ${action.command.split('.').pop()}`];
  blocks.push('```bash\n' + usageLine(action) + '\n```');
  if (action.description) blocks.push(action.description);

  const meta = [t.kindRisk(action.kind, action.risk?.level ?? '')];
  const c = action.contract;
  if (c?.wait?.waitable && c.wait.waiters?.[0]) meta.push(t.wait(c.wait.waiters[0]));
  if (c?.idempotency?.supported) meta.push(t.idem(c.idempotency.field));
  if (c?.dry_run?.supported) meta.push(t.dryRun);
  blocks.push(meta.join('\n'));

  const calls = apiTable(action, t);
  if (calls) blocks.push(calls);

  const table = paramRows(action, t);
  if (table) blocks.push(table);
  return blocks.join('\n\n');
}

function resourcePage(product, resource, t) {
  const commandPath = [product];
  if (resource.parent) commandPath.push(resource.parent);
  if (resource.parent || resource.name !== product) commandPath.push(resource.name);
  const cmd = commandPath.join(' ');
  const dotted = resource.schema_id;
  if (!dotted) throw new Error(`resource ${product}/${resource.name} is missing schema_id`);
  const title = cmd;
  const blocks = [
    ['---', `title: ${title}`, `sidebar_label: ${resourceReferenceLabel(resource)}`, `description: ${JSON.stringify(resource.description || title)}`, '---'].join('\n'),
    `# ${title}`,
  ];
  if (resource.description) blocks.push(resource.description);
  blocks.push(t.intro(cmd, dotted));
  for (const name of orderedActions(resource.actions)) {
    blocks.push(actionSection(schema([`${dotted}.${name}`, '--full'], t._lang), t));
  }
  return blocks.join('\n\n') + '\n';
}

function run() {
  for (const locale of LOCALES) {
    const t = {...locale.t, _lang: locale.lang};
    rmSync(locale.dir, {recursive: true, force: true});
    mkdirSync(locale.dir, {recursive: true});

    const products = schema(['--list'], locale.lang).products;
    products.forEach((p, index) => {
      const productDir = join(locale.dir, p.name);
      mkdirSync(productDir, {recursive: true});
      // A generated-index page turns each product into an index of its resources.
      writeFileSync(
        join(productDir, '_category_.json'),
        JSON.stringify(
          {
            label: p.name.toUpperCase(),
            position: index + 1,
            link: {
              type: 'generated-index',
              title: t.indexTitle(p.name.toUpperCase()),
              description: p.description || '',
            },
          },
          null,
          2,
        ) + '\n',
      );
      const detail = schema(['--list', p.name], locale.lang);
      for (const resource of detail.resources) {
        const page = resourcePage(p.name, resource, t);
        writeFileSync(join(productDir, resourceReferenceFilename(resource)), page);
      }
      console.log(`[${locale.lang}] ${p.name}: ${detail.resources.length} resources`);
    });
  }
  writeZhCategoryTranslations();
  console.log('reference pages generated.');
}

// Docusaurus translates generated-index page title/description through the docs
// translation file, not the localized _category_.json, so write them here to
// keep the zh-Hans product index pages in sync with the specs on every run.
function writeZhCategoryTranslations() {
  const path = join(WEBSITE_DIR, 'i18n', 'zh-Hans', 'docusaurus-plugin-content-docs', 'current.json');
  const data = JSON.parse(readFileSync(path, 'utf8'));
  const zh = LOCALES.find((l) => l.lang === 'zh-CN').t;
  for (const p of schema(['--list'], 'zh-CN').products) {
    const code = p.name.toUpperCase();
    const key = `sidebar.docsSidebar.category.${code}`;
    data[key] = {message: code, description: `The label for category '${code}' in sidebar 'docsSidebar'`};
    data[`${key}.link.generated-index.title`] = {
      message: zh.indexTitle(code),
      description: `The generated-index page title for category '${code}' in sidebar 'docsSidebar'`,
    };
    data[`${key}.link.generated-index.description`] = {
      message: p.description || '',
      description: `The generated-index page description for category '${code}' in sidebar 'docsSidebar'`,
    };
  }
  writeFileSync(path, JSON.stringify(data, null, 2) + '\n');
}

run();

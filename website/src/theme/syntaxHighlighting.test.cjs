const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const websiteRoot = path.resolve(__dirname, '../..');
const customCss = fs.readFileSync(path.join(websiteRoot, 'src/css/custom.css'), 'utf8');
const packageJson = require(path.join(websiteRoot, 'package.json'));

test('declares PrismJS because the custom theme imports it directly', () => {
  assert.match(packageJson.dependencies.prismjs, /^\^?1\./);
});

test('neutralizes highlighted Bash scalar values only when they follow an option', () => {
  for (const tokenType of ['number', 'boolean', 'function']) {
    assert.match(
      customCss,
      new RegExp(
        String.raw`\.language-bash \.token\.parameter \+ \.token\.plain \+ \.token\.${tokenType}`,
      ),
    );
  }

  assert.match(customCss, /color:\s*inherit\s*!important;/);
});

test('gives JSON property tokens a distinct dark-theme color', () => {
  assert.match(
    customCss,
    /\[data-theme='dark'\] \.language-json \.token\.property\s*\{[^}]*color:\s*#8be9fd;/s,
  );
});

test('uses JSON fences for JSON-shaped examples in both locales', () => {
  const docs = [
    path.join(websiteRoot, 'docs/user-guide/common-differences.md'),
    path.join(
      websiteRoot,
      'i18n/zh-Hans/docusaurus-plugin-content-docs/current/user-guide/common-differences.md',
    ),
  ];

  for (const doc of docs) {
    assert.doesNotMatch(fs.readFileSync(doc, 'utf8'), /```text\r?\n\{/);
  }
});

test('uses JSON highlighting for merged security-group reference examples', () => {
  const docs = [
    {
      path: path.join(websiteRoot, 'docs/user-guide/resource-optimizations/ecs.md'),
      marker:
        'The two OpenAPI responses remain separate. ecctl merges the references into the',
    },
    {
      path: path.join(
        websiteRoot,
        'i18n/zh-Hans/docusaurus-plugin-content-docs/current/user-guide/resource-optimizations/ecs.md',
      ),
      marker: '两个 OpenAPI 响应相互独立。ecctl 将引用关系合并到安全组视图中：',
    },
  ];

  for (const doc of docs) {
    const content = fs.readFileSync(doc.path, 'utf8');
    const sectionStart = content.indexOf(doc.marker);

    assert.notEqual(sectionStart, -1, `missing security-group example in ${doc.path}`);

    const fenceStart = content.indexOf('```', sectionStart);
    const fenceEnd = content.indexOf('```', fenceStart + 3);
    const section = content.slice(sectionStart, fenceEnd + 3);

    assert.match(section, /```json\r?\n/);
    assert.match(section, /\/\/ DescribeSecurityGroupAttribute/);
    assert.match(section, /\/\/ DescribeSecurityGroupReferences/);
    assert.match(section, /\/\/ ecctl/);
  }
});

test('uses JSON fences and JSON comments for resource-optimization responses', () => {
  const docRoots = [
    path.join(websiteRoot, 'docs/user-guide/resource-optimizations'),
    path.join(
      websiteRoot,
      'i18n/zh-Hans/docusaurus-plugin-content-docs/current/user-guide/resource-optimizations',
    ),
  ];

  for (const docRoot of docRoots) {
    const docs = fs.readdirSync(docRoot).filter((name) => name.endsWith('.md'));

    for (const name of docs) {
      const docPath = path.join(docRoot, name);
      const content = fs.readFileSync(docPath, 'utf8');
      const fences = content.matchAll(/^```([^\r\n]*)\r?\n([\s\S]*?)^```$/gm);

      for (const [, language, body] of fences) {
        if (language === 'text') {
          assert.doesNotMatch(
            body,
            /^\s*\{/m,
            `JSON-shaped text fence remains in ${docPath}`,
          );
        }

        if (language === 'json') {
          assert.doesNotMatch(
            body,
            /^# /m,
            `shell-style label remains in JSON fence in ${docPath}`,
          );
        }
      }
    }
  }
});

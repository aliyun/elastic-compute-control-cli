import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {fileURLToPath} from 'node:url';

const websiteDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');

const docs = {
  products: 'docs/reference/products.md',
  english: {
    quickStart: 'docs/getting-started/quick-start.md',
    discovery: 'docs/user-guide/discovery.md',
    quickStartHeading: 'List Products',
    discoveryHeading: 'Products',
  },
  chinese: {
    quickStart: 'i18n/zh-Hans/docusaurus-plugin-content-docs/current/getting-started/quick-start.md',
    discovery: 'i18n/zh-Hans/docusaurus-plugin-content-docs/current/user-guide/discovery.md',
    quickStartHeading: '列出产品',
    discoveryHeading: '产品',
  },
};

async function read(relativePath) {
  return readFile(path.join(websiteDir, relativePath), 'utf8');
}

function section(markdown, heading) {
  const marker = `## ${heading}\n`;
  const start = markdown.indexOf(marker);
  assert.notEqual(start, -1, `missing ${marker.trim()} section`);
  const contentStart = start + marker.length;
  const nextHeading = markdown.indexOf('\n## ', contentStart);
  return markdown.slice(contentStart, nextHeading === -1 ? undefined : nextHeading);
}

function generatedProductIds(markdown) {
  return [...markdown.matchAll(/^## \[([^\]]+)\]\([^\n]+\)$/gm)]
    .map((match) => match[1].toLowerCase());
}

function quickStartProductIds(markdown, heading) {
  return [...section(markdown, heading).matchAll(/^\| `([^`]+)` \|/gm)]
    .map((match) => match[1]);
}

function discoveryProductIds(markdown, heading) {
  const productsSection = section(markdown, heading);
  const jsonBlock = productsSection.match(/```json\n([\s\S]*?)\n```/);
  assert.ok(jsonBlock, `missing JSON product example in ${heading} section`);
  return JSON.parse(jsonBlock[1]).products.map((product) => product.name);
}

test('English and Chinese public-surface docs match generated products', async () => {
  const expected = generatedProductIds(await read(docs.products));
  assert.ok(expected.length > 0, 'generated product index must contain products');

  for (const [language, paths] of Object.entries({
    English: docs.english,
    Chinese: docs.chinese,
  })) {
    assert.deepEqual(
      quickStartProductIds(await read(paths.quickStart), paths.quickStartHeading),
      expected,
      `${language} Quick Start product table drifted from generated products`,
    );
    assert.deepEqual(
      discoveryProductIds(await read(paths.discovery), paths.discoveryHeading),
      expected,
      `${language} discovery example drifted from generated products`,
    );
  }
});

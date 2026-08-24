import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import test from 'node:test';

const homepageStyles = await readFile(
  new URL('../src/pages/index.module.css', import.meta.url),
  'utf8',
);

function ruleBody(selector) {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const match = homepageStyles.match(new RegExp(`${escapedSelector}\\s*\\{([^}]*)\\}`));
  assert.ok(match, `missing CSS rule for ${selector}`);
  return match[1];
}

test('homepage install command wraps without scrolling or truncation', () => {
  const commandRule = ruleBody('.commandBar code');

  assert.match(commandRule, /overflow-wrap:\s*anywhere;/);
  assert.match(commandRule, /white-space:\s*normal;/);
  assert.doesNotMatch(commandRule, /overflow-x:\s*auto;/);
  assert.doesNotMatch(commandRule, /text-overflow:\s*ellipsis;/);
});

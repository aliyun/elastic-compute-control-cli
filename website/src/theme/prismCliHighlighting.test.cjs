const test = require('node:test');
const assert = require('node:assert/strict');
const {Prism} = require('prism-react-renderer');
const {customizeBashCliHighlighting} = require('./prismCliHighlighting.cjs');

test('highlights hyphenated long options without changing existing Bash tokens', () => {
  const PrismBefore = globalThis.Prism;
  globalThis.Prism = Prism;
  require('prismjs/components/prism-bash.js');
  delete globalThis.Prism;
  if (typeof PrismBefore !== 'undefined') {
    globalThis.Prism = PrismBefore;
  }

  customizeBashCliHighlighting(Prism);
  const html = Prism.highlight(
    'ecctl vpc delete --dry-run\necctl list --page 1 -r "demo"',
    Prism.languages.bash,
    'bash',
  );

  assert.match(html, /token parameter variable">--dry-run/);
  assert.match(html, /token parameter variable">--page/);
  assert.match(html, /token number">1/);
  assert.match(html, /token parameter variable">-r/);
  assert.match(html, /token string">"demo"/);
});

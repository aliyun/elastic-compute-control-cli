const assert = require('node:assert/strict');
const test = require('node:test');

const {
  getBrowserLocaleRedirect,
  selectPreferredLocale,
} = require('./localeRedirectRules.cjs');

const config = {
  baseUrl: '/ecctl/',
  defaultLocale: 'en',
  locales: ['en', 'zh-Hans'],
};

test('selectPreferredLocale maps Chinese browser language variants to zh-Hans', () => {
  assert.equal(selectPreferredLocale(['zh-CN', 'en-US'], config), 'zh-Hans');
  assert.equal(selectPreferredLocale(['zh-Hans-CN', 'en-US'], config), 'zh-Hans');
});

test('selectPreferredLocale falls back to the default locale for unsupported languages', () => {
  assert.equal(selectPreferredLocale(['ja-JP'], config), 'en');
});

test('getBrowserLocaleRedirect redirects only the root page to a non-default browser locale', () => {
  assert.equal(
    getBrowserLocaleRedirect({
      ...config,
      pathname: '/ecctl/',
      search: '?utm_source=test',
      hash: '#top',
      browserLanguages: ['zh-CN', 'en-US'],
      alreadyRedirected: false,
    }),
    '/ecctl/zh-Hans/?utm_source=test#top',
  );
});

test('getBrowserLocaleRedirect skips localized, non-root, default-locale, and already-redirected pages', () => {
  assert.equal(
    getBrowserLocaleRedirect({
      ...config,
      pathname: '/ecctl/zh-Hans/docs/intro',
      search: '',
      hash: '',
      browserLanguages: ['zh-CN'],
      alreadyRedirected: false,
    }),
    null,
  );
  assert.equal(
    getBrowserLocaleRedirect({
      ...config,
      baseUrl: '/ecctl/zh-Hans/',
      pathname: '/ecctl/zh-Hans/',
      search: '',
      hash: '',
      browserLanguages: ['zh-CN'],
      alreadyRedirected: false,
    }),
    null,
  );
  assert.equal(
    getBrowserLocaleRedirect({
      ...config,
      pathname: '/ecctl/docs/intro',
      search: '',
      hash: '',
      browserLanguages: ['zh-CN'],
      alreadyRedirected: false,
    }),
    null,
  );
  assert.equal(
    getBrowserLocaleRedirect({
      ...config,
      pathname: '/ecctl/',
      search: '',
      hash: '',
      browserLanguages: ['en-US'],
      alreadyRedirected: false,
    }),
    null,
  );
  assert.equal(
    getBrowserLocaleRedirect({
      ...config,
      pathname: '/ecctl/',
      search: '',
      hash: '',
      browserLanguages: ['zh-CN'],
      alreadyRedirected: true,
    }),
    null,
  );
});

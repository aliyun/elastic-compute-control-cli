function normalizeBaseUrl(baseUrl) {
  const withLeadingSlash = baseUrl.startsWith('/') ? baseUrl : `/${baseUrl}`;
  return withLeadingSlash.endsWith('/') ? withLeadingSlash : `${withLeadingSlash}/`;
}

function normalizePathname(pathname) {
  const withLeadingSlash = pathname.startsWith('/') ? pathname : `/${pathname}`;
  return withLeadingSlash.endsWith('/index.html')
    ? withLeadingSlash.slice(0, -'index.html'.length)
    : withLeadingSlash;
}

function resolveConfiguredLocale(browserLanguage, locales) {
  const normalized = browserLanguage.toLowerCase().replace('_', '-');
  const localeByLowercase = new Map(locales.map((locale) => [locale.toLowerCase(), locale]));

  if (normalized.startsWith('zh') && localeByLowercase.has('zh-hans')) {
    return localeByLowercase.get('zh-hans');
  }

  const exactLocale = localeByLowercase.get(normalized);
  if (exactLocale) {
    return exactLocale;
  }

  const language = normalized.split('-')[0];
  return localeByLowercase.get(language) ?? null;
}

function selectPreferredLocale(browserLanguages, config) {
  for (const browserLanguage of browserLanguages) {
    const locale = resolveConfiguredLocale(browserLanguage, config.locales);
    if (locale) {
      return locale;
    }
  }

  return config.defaultLocale;
}

function isRootPath(pathname, baseUrl) {
  const normalizedPathname = normalizePathname(pathname);
  return normalizedPathname === normalizeBaseUrl(baseUrl);
}

function baseUrlIncludesLocale(baseUrl, locales) {
  const normalizedBaseUrl = normalizeBaseUrl(baseUrl);
  const segments = normalizedBaseUrl.split('/').filter(Boolean);
  const lastSegment = segments.at(-1);
  if (!lastSegment) {
    return false;
  }

  const localesByLowercase = new Set(locales.map((locale) => locale.toLowerCase()));
  return localesByLowercase.has(lastSegment.toLowerCase());
}

function getBrowserLocaleRedirect({
  pathname,
  search = '',
  hash = '',
  browserLanguages,
  alreadyRedirected,
  ...config
}) {
  if (baseUrlIncludesLocale(config.baseUrl, config.locales)) {
    return null;
  }

  if (alreadyRedirected || !isRootPath(pathname, config.baseUrl)) {
    return null;
  }

  const preferredLocale = selectPreferredLocale(browserLanguages, config);
  if (preferredLocale === config.defaultLocale) {
    return null;
  }

  return `${normalizeBaseUrl(config.baseUrl)}${preferredLocale}/${search}${hash}`;
}

module.exports = {
  getBrowserLocaleRedirect,
  selectPreferredLocale,
};

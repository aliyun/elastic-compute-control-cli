import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const config: Config = {
  title: 'ecctl',
  tagline: 'An Agent-first command-line controller for Alibaba Cloud resource operations',
  favicon: 'img/favicon.png',

  url: process.env.SITE_URL ?? 'https://aliyun.github.io',
  baseUrl: process.env.BASE_URL ?? '/ecctl/',

  organizationName: 'aliyun',
  projectName: 'ecctl',
  clientModules: ['./src/clientModules/localeRedirect.js'],

  onBrokenLinks: 'throw',
  markdown: {
    hooks: {
      onBrokenMarkdownLinks: 'warn',
    },
  },

  i18n: {
    defaultLocale: 'en',
    locales: ['en', 'zh-Hans'],
    localeConfigs: {
      en: {
        label: 'English',
        direction: 'ltr',
        htmlLang: 'en-US',
      },
      'zh-Hans': {
        label: '简体中文',
        direction: 'ltr',
        htmlLang: 'zh-CN',
      },
    },
  },

  themes: [
    [
      '@easyops-cn/docusaurus-search-local',
      {
        hashed: true,
        language: ['en', 'zh'],
        indexBlog: false,
      },
    ],
  ],

  presets: [
    [
      'classic',
      {
        docs: {
          sidebarPath: './sidebars.ts',
          routeBasePath: 'docs',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    image: 'img/logo.png',
    navbar: {
      title: 'ecctl',
      logo: {
        alt: 'ecctl logo',
        src: 'img/logo.png',
      },
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'docsSidebar',
          position: 'left',
          label: 'Docs',
        },
        {
          to: '/docs/user-guide/discovery',
          label: 'Schema',
          position: 'left',
        },
        {
          to: '/docs/reference/resource-coverage',
          label: 'Resources',
          position: 'left',
        },
        {
          type: 'localeDropdown',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'light',
      links: [
        {
          title: 'Docs',
          items: [
            {
              label: 'Getting Started',
              to: '/docs/getting-started/installation',
            },
            {
              label: 'User Guide',
              to: '/docs/user-guide/command-model',
            },
            {
              label: 'Resource Coverage',
              to: '/docs/reference/resource-coverage',
            },
          ],
        },
        {
          title: 'Reference',
          items: [
            {
              label: 'Command Reference',
              to: '/docs/reference/commands',
            },
            {
              label: 'Error Model',
              to: '/docs/reference/errors',
            },
            {
              label: 'Resource Specs',
              to: '/docs/contributing/resource-specs',
            },
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} Alibaba Cloud.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ['bash', 'json', 'yaml', 'go'],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;

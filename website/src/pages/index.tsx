import {type ReactNode, useState} from 'react';
import clsx from 'clsx';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import styles from './index.module.css';

type Locale = 'en' | 'zh-Hans';

type Feature = {
  title: string;
  body: string;
  command?: string;
};

type Stat = {
  value: string;
  label: string;
};

type HomeCopy = {
  title: string;
  subtitle: string;
  installCommand: string;
  copyCommand: string;
  copiedCommand: string;
  docsCta: string;
  githubCta: string;
  terminalTitle: string;
  terminalLines: Array<{kind: 'command' | 'json' | 'text' | 'ok'; content: ReactNode}>;
  stats: Stat[];
  whyTitle: string;
  whyEyebrow: string;
  whyItems: Feature[];
  workflowTitle: string;
  workflowEyebrow: string;
  workflowLead: string;
  workflowItems: Feature[];
  ctaTitle: string;
  ctaBody: string;
};

type CommandToken = {
  text: string;
  kind: 'prompt' | 'binary' | 'argument' | 'flag' | 'value' | 'plain';
};

const installCommand = 'go install github.com/aliyun/ecctl/cmd/ecctl@latest';

const terminalResultLines: HomeCopy['terminalLines'] = [
  {kind: 'json', content: '{'},
  {
    kind: 'json',
    content: (
      <>
        {' '}
        <span className={styles.jsonKey}>"vpc"</span>
        {': {'}
      </>
    ),
  },
  {
    kind: 'json',
    content: (
      <>
        {'   '}
        <span className={styles.jsonKey}>"id"</span>
        {': '}
        <span className={styles.jsonString}>"vpc-2zeo51…"</span>
        {','}
      </>
    ),
  },
  {
    kind: 'json',
    content: (
      <>
        {'   '}
        <span className={styles.jsonKey}>"status"</span>
        {': '}
        <span className={styles.jsonString}>"Available"</span>
        {','}
      </>
    ),
  },
  {
    kind: 'json',
    content: (
      <>
        {'   '}
        <span className={styles.jsonEllipsis}>...</span>
      </>
    ),
  },
  {kind: 'json', content: ' },'},
  {
    kind: 'json',
    content: (
      <>
        {' '}
        <span className={styles.jsonEllipsis}>...</span>
      </>
    ),
  },
  {kind: 'json', content: '}'},
];

function highlightCommand(command: string): CommandToken[] {
  return command.split(/(\s+)/).map((text, index) => {
    if (/^\s+$/.test(text)) {
      return {text, kind: 'plain'};
    }
    if (text === '$') {
      return {text, kind: 'prompt'};
    }
    if (text === 'ecctl' || (index === 0 && text === 'go')) {
      return {text, kind: 'binary'};
    }
    if (text.startsWith('-')) {
      return {text, kind: 'flag'};
    }
    if (text.includes('=') || text.includes('/')) {
      return {text, kind: 'value'};
    }
    return {text, kind: 'argument'};
  });
}

const copyByLocale: Record<Locale, HomeCopy> = {
  en: {
    title: 'ecctl',
    subtitle:
      'The Alibaba Cloud elastic computing CLI, built for agents — operate by resource, return structured JSON, and wait for the result by default.',
    installCommand,
    copyCommand: 'Copy command',
    copiedCommand: 'Copied',
    docsCta: 'Read the docs',
    githubCta: 'GitHub',
    terminalTitle: 'ecctl vpc create',
    terminalLines: [
      {kind: 'ok', content: '# created, waited for Available, read back — one command'},
      {kind: 'command', content: '$ ecctl vpc create --name demo'},
      ...terminalResultLines,
    ],
    stats: [
      {value: 'Resources', label: 'One grammar across all products'},
      {value: 'Inspect first', label: 'Parameters and risk before you run'},
      {value: 'Synchronous', label: 'Returns when the resource is ready'},
      {value: 'Structured output', label: 'Results and errors, all JSON'},
    ],
    whyTitle: 'Designed for agents, not just terminals',
    whyEyebrow: 'Why ecctl',
    whyItems: [
      {
        title: 'Operate by resource, not endpoint',
        body: 'Operate Alibaba Cloud elastic computing resources through one product/resource/action grammar, without memorizing OpenAPI operation names or parameter expansion.',
      },
      {
        title: 'Inspect a command before running it',
        body: 'Before running, inspect a command’s required parameters, risk level, and waiting behavior.',
      },
      {
        title: 'Synchronous results',
        body: 'Asynchronous operations wait for the resource to be ready and read it back, so a command returns the final result rather than a pending status.',
      },
      {
        title: 'JSON results and errors',
        body: 'Output is JSON by default; failures are structured JSON too, with an error code, a retryable flag, and the cloud request_id.',
      },
      {
        title: 'Safe by default',
        body: 'Validate with a dry run, stay idempotent to avoid duplicate creates, and require confirmation before destructive deletes.',
      },
      {
        title: 'Fallback for the long tail',
        body: 'Common operations have ready-made commands; capabilities not yet modeled remain reachable through a raw OpenAPI call.',
      },
    ],
    workflowTitle: 'Run commands; look up details when you need them',
    workflowEyebrow: 'Everyday use',
    workflowLead:
      'Commands are organized by product, resource, and action, so most of the time you just run them.',
    workflowItems: [
      {
        title: 'Run the command',
        body: 'Resource commands have a regular, predictable shape, so common operations run directly.',
        command: 'ecctl ecs instance list --filter status=Running',
      },
      {
        title: 'Add -h for flags',
        body: 'Append -h to any command to see its usage, required fields, and flags.',
        command: 'ecctl ecs instance create -h',
      },
      {
        title: 'Use schema for the full spec',
        body: 'For complex parameters or automation, schema returns every parameter and behavior as structured JSON an agent can read.',
        command: 'ecctl schema ecs.instance.create --brief',
      },
    ],
    ctaTitle: 'Operate Alibaba Cloud elastic computing with one set of resource commands',
    ctaBody:
      'Follow the quick start to install ecctl and run your first resource command.',
  },
  'zh-Hans': {
    title: 'ecctl',
    subtitle:
      '为 Agent 而生的阿里云弹性计算命令行 —— 面向资源操作，默认输出 JSON，创建后等待资源就绪再返回。',
    installCommand,
    copyCommand: '复制命令',
    copiedCommand: '已复制',
    docsCta: '阅读文档',
    githubCta: 'GitHub',
    terminalTitle: 'ecctl vpc create',
    terminalLines: [
      {kind: 'ok', content: '# 一条命令：创建、等到 Available、回读资源'},
      {kind: 'command', content: '$ ecctl vpc create --name demo'},
      ...terminalResultLines,
    ],
    stats: [
      {value: '面向资源', label: '统一语法操作所有产品'},
      {value: '执行前可查', label: '必填参数、风险与等待行为'},
      {value: '同步返回', label: '资源就绪后返回最终结果'},
      {value: '结构化输出', label: '结果与错误均为 JSON'},
    ],
    whyTitle: '为 Agent 设计，而不仅是终端',
    whyEyebrow: '为什么是 ecctl',
    whyItems: [
      {
        title: '面向资源，而非接口',
        body: '以统一的 product/resource/action 语法操作阿里云弹性计算资源，无需记忆 OpenAPI 操作名与参数展开。',
      },
      {
        title: '执行前可查看命令要求',
        body: '运行前即可查看命令的必填参数、风险等级与等待行为。',
      },
      {
        title: '异步操作同步返回',
        body: '异步操作默认等待资源就绪并回读，命令返回最终结果，而非“已提交”状态。',
      },
      {
        title: '结果与错误均为 JSON',
        body: '默认输出 JSON；错误亦为结构化 JSON，包含错误码、是否可重试与云端 request_id。',
      },
      {
        title: '更安全的变更',
        body: '支持 dry-run 校验、通过幂等避免重复创建，破坏性删除需显式确认。',
      },
      {
        title: '长尾能力可回退',
        body: '常用操作提供现成命令；尚未建模的能力可直接发起原始 OpenAPI 调用。',
      },
    ],
    workflowTitle: '直接运行命令，需要时再查细节',
    workflowEyebrow: '日常使用',
    workflowLead:
      '命令按产品、资源、动作组织，形态可预期，大多数时候直接运行即可。',
    workflowItems: [
      {
        title: '直接运行命令',
        body: '资源命令形态统一、可预期，常见操作可直接运行。',
        command: 'ecctl ecs instance list --filter status=Running',
      },
      {
        title: '加 -h 查看参数',
        body: '任意命令追加 -h，即可查看用法、必填项和 flag。',
        command: 'ecctl ecs instance create -h',
      },
      {
        title: '用 schema 看完整规格',
        body: '面对复杂参数或自动化场景，schema 以结构化 JSON 返回每个参数与行为，便于 Agent 读取和填参。',
        command: 'ecctl schema ecs.instance.create --brief',
      },
    ],
    ctaTitle: '用一套资源命令操作阿里云弹性计算产品',
    ctaBody: '按快速开始安装 ecctl，运行你的第一个资源命令。',
  },
};

function ArrowIcon() {
  return (
    <svg
      width="18"
      height="18"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <line x1="5" y1="12" x2="19" y2="12" />
      <polyline points="12 5 19 12 12 19" />
    </svg>
  );
}

function GitHubIcon() {
  return (
    <svg
      width="20"
      height="20"
      viewBox="0 0 24 24"
      fill="currentColor"
      aria-hidden="true"
    >
      <path d="M12 .5C5.73.5.5 5.73.5 12c0 5.08 3.29 9.39 7.86 10.91.58.11.79-.25.79-.56 0-.28-.01-1.02-.02-2-3.2.7-3.88-1.54-3.88-1.54-.52-1.33-1.28-1.69-1.28-1.69-1.05-.72.08-.7.08-.7 1.16.08 1.77 1.19 1.77 1.19 1.03 1.77 2.7 1.26 3.36.96.1-.75.4-1.26.73-1.55-2.55-.29-5.23-1.28-5.23-5.69 0-1.26.45-2.29 1.19-3.1-.12-.29-.52-1.46.11-3.05 0 0 .97-.31 3.18 1.18a11.1 11.1 0 0 1 2.9-.39c.98 0 1.97.13 2.9.39 2.2-1.49 3.17-1.18 3.17-1.18.63 1.59.23 2.76.11 3.05.74.81 1.19 1.84 1.19 3.1 0 4.42-2.69 5.39-5.25 5.68.41.36.78 1.06.78 2.14 0 1.55-.01 2.8-.01 3.18 0 .31.21.68.8.56A10.52 10.52 0 0 0 23.5 12C23.5 5.73 18.27.5 12 .5z" />
    </svg>
  );
}

function CheckIcon() {
  return (
    <svg
      className={styles.checkIcon}
      width="20"
      height="20"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="3"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <polyline points="20 6 9 17 4 12" />
    </svg>
  );
}

function getLocaleCopy(locale: string): HomeCopy {
  return copyByLocale[(locale as Locale) in copyByLocale ? (locale as Locale) : 'en'];
}

function CopyInstallCommand({copy}: {copy: HomeCopy}) {
  const [copied, setCopied] = useState(false);

  async function handleCopy() {
    try {
      await navigator.clipboard.writeText(copy.installCommand);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    } catch {
      setCopied(false);
    }
  }

  return (
    <div className={styles.installPanel}>
      <div className={styles.commandBar}>
        <span className={styles.prompt}>$</span>
        <CommandLine command={copy.installCommand} />
        <button
          type="button"
          aria-label={copied ? copy.copiedCommand : copy.copyCommand}
          title={copied ? copy.copiedCommand : copy.copyCommand}
          className={clsx(styles.copyButton, copied && styles.copyButtonCopied)}
          onClick={handleCopy}
        />
      </div>
    </div>
  );
}

function CommandLine({command, className}: {command: string; className?: string}) {
  return (
    <code className={clsx(styles.commandLine, className)}>
      {highlightCommand(command).map((token, index) => (
        <span key={`${token.text}-${index}`} className={styles[`commandToken_${token.kind}`]}>
          {token.text}
        </span>
      ))}
    </code>
  );
}

function Terminal({copy}: {copy: HomeCopy}) {
  return (
    <div className={styles.terminal}>
      <div className={styles.terminalToolbar}>
        <span />
        <span />
        <span />
      </div>
      <div className={styles.terminalStream}>
        {copy.terminalLines.map((line, index) => {
          const content =
            line.kind === 'command' && typeof line.content === 'string' ? (
              <CommandLine command={line.content} className={styles.terminalCommand} />
            ) : (
              line.content
            );
          return (
            <div
              key={index}
              className={clsx(styles.terminalLine, styles[`terminalLine_${line.kind}`])}
            >
              {content}
            </div>
          );
        })}
      </div>
    </div>
  );
}

function StatsBar({stats}: {stats: Stat[]}) {
  return (
    <section className={styles.statsSection}>
      <div className={styles.statsGrid}>
        {stats.map((stat) => (
          <div className={styles.statItem} key={`${stat.value}-${stat.label}`}>
            <span className={styles.statValue}>{stat.value}</span>
            <span className={styles.statLabel}>{stat.label}</span>
          </div>
        ))}
      </div>
    </section>
  );
}

function FeatureGrid({items}: {items: Feature[]}) {
  return (
    <div className={styles.featureGrid}>
      {items.map((item, index) => (
        <article className={styles.featureCard} key={item.title}>
          <div className={styles.featureIcon}>{String(index + 1).padStart(2, '0')}</div>
          <h3>{item.title}</h3>
          <p>{item.body}</p>
        </article>
      ))}
    </div>
  );
}

function WorkflowSection({copy}: {copy: HomeCopy}) {
  return (
    <section className={styles.standardize}>
      <div className={styles.standardizeInner}>
        <div className={styles.standardizeCopy}>
          <span className={styles.eyebrow}>{copy.workflowEyebrow}</span>
          <h2>{copy.workflowTitle}</h2>
          <p>{copy.workflowLead}</p>
          <ul className={styles.checkList}>
            {copy.workflowItems.map((item) => (
              <li key={item.title}>
                <CheckIcon />
                <span>
                  <strong>{item.title}</strong>
                  {item.body}
                </span>
              </li>
            ))}
          </ul>
          <Link
            className={clsx(styles.btn, styles.btnPrimary)}
            to="/docs/getting-started/quick-start"
          >
            {copy.docsCta}
            <ArrowIcon />
          </Link>
        </div>
        <div className={styles.standardizeVisual}>
          <div className={styles.flowCard}>
            {copy.workflowItems.map((item) => (
              <div className={styles.flowStep} key={item.title}>
                <strong>{item.title}</strong>
                {item.command ? <CommandLine command={item.command} className={styles.flowCommand} /> : null}
              </div>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}

export default function Home(): ReactNode {
  const {siteConfig, i18n} = useDocusaurusContext();
  const copy = getLocaleCopy(i18n.currentLocale);

  return (
    <Layout title={copy.title} description={siteConfig.tagline}>
      <main className={styles.home}>
        <section className={styles.hero}>
          <div className={styles.heroGlow} aria-hidden="true" />
          <div className={styles.heroInner}>
            <div className={styles.heroCopy}>
              <h1>{copy.title}</h1>
              <p>{copy.subtitle}</p>
              <CopyInstallCommand copy={copy} />
              <div className={styles.heroActions}>
                <Link className={clsx(styles.btn, styles.btnPrimary)} to="/docs/intro">
                  {copy.docsCta}
                  <ArrowIcon />
                </Link>
                <Link
                  className={clsx(styles.btn, styles.btnSecondary)}
                  to="https://github.com/aliyun/elastic-compute-control-cli"
                >
                  <GitHubIcon />
                  {copy.githubCta}
                </Link>
              </div>
            </div>
            <div className={styles.heroVisual} aria-label={copy.terminalTitle}>
              <Terminal copy={copy} />
            </div>
          </div>
        </section>

        <StatsBar stats={copy.stats} />

        <section className={styles.section}>
          <div className={styles.sectionInner}>
            <span className={styles.eyebrow}>{copy.whyEyebrow}</span>
            <h2>{copy.whyTitle}</h2>
            <FeatureGrid items={copy.whyItems} />
          </div>
        </section>

        <WorkflowSection copy={copy} />

        <section className={styles.ctaBand}>
          <div className={styles.ctaInner}>
            <h2>{copy.ctaTitle}</h2>
            <p>{copy.ctaBody}</p>
            <Link
              className={clsx(styles.btn, styles.btnPrimary)}
              to="/docs/getting-started/quick-start"
            >
              {copy.docsCta}
              <ArrowIcon />
            </Link>
          </div>
        </section>
      </main>
    </Layout>
  );
}

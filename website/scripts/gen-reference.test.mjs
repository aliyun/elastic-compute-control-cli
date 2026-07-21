import assert from 'node:assert/strict';
import test from 'node:test';

import {apiTable, escCell} from './gen-reference-lib.mjs';

const english = {
  apiHeader: '| API | When called | Purpose |\n|---|---|---|',
  repeated: 'repeated',
  cached: 'cached; no additional request',
  annotate: (purpose, note) => `${purpose} (${note})`,
};

const chinese = {
  apiHeader: '| API | 调用时机 | 用途 |\n|---|---|---|',
  repeated: '重复调用',
  cached: '复用等待结果，不额外请求',
  annotate: (purpose, note) => `${purpose}（${note}）`,
};

const action = {
  api_calls: [
    {
      api: 'CreateThing',
      phase: 'operation',
      condition_description: 'Every time the command runs.',
      purpose: 'Create the thing.',
    },
    {
      api: 'DescribeThing',
      phase: 'wait',
      condition: '!(input.no_wait)',
      condition_description: 'When `--no-wait` is not specified.',
      purpose: 'Wait for the thing.',
      repeated: true,
    },
    {
      api: 'DescribeThing',
      phase: 'readback',
      condition: '!(input.no_wait)',
      condition_description: 'When `--no-wait` is not specified.',
      purpose: 'Return the final thing.',
      cached: true,
    },
  ],
};

test('escCell escapes existing backslashes before Markdown table separators', () => {
  assert.equal(
    escCell(String.raw`C:\tmp\file|next`),
    String.raw`C:\\tmp\\file\|next`,
  );
});

test('apiTable renders English conditions, repeated polling, and cached readback', () => {
  const got = apiTable(action, english);
  assert.match(got, /^\| API \| When called \| Purpose \|/);
  assert.match(got, /\| `CreateThing` \| Every time the command runs\. \| Create the thing\. \|/);
  assert.match(got, /When `--no-wait` is not specified\. \| Wait for the thing\. \(repeated\)/);
  assert.match(got, /Return the final thing\. \(cached; no additional request\)/);
});

test('apiTable renders Chinese condition and execution annotations', () => {
  const localized = {
    ...action,
    api_calls: action.api_calls.map((call) => ({
      ...call,
      condition_description: call.condition ? '未指定 `--no-wait` 时' : '每次执行命令时',
      purpose: '用途',
    })),
  };
  const got = apiTable(localized, chinese);
  assert.match(got, /^\| API \| 调用时机 \| 用途 \|/);
  assert.match(got, /未指定 `--no-wait` 时 \| 用途（重复调用）/);
  assert.match(got, /用途（复用等待结果，不额外请求）/);
});

test('apiTable omits empty API call sections', () => {
  assert.equal(apiTable({api_calls: []}, english), '');
  assert.equal(apiTable({}, english), '');
});

test('apiTable renders the schema condition description instead of raw DSL', () => {
  const got = apiTable({
    api_calls: [{
      api: 'UpdateThing',
      phase: 'wait',
      condition: '(has(input.name) || has(input.description)) && !(input.no_wait)',
      condition_description: 'When either `--name` or `--description` is specified and `--no-wait` is not specified.',
      purpose: 'Wait for the thing.',
    }],
  }, english);

  assert.match(got, /When either `--name` or `--description` is specified and `--no-wait` is not specified\./);
  assert.doesNotMatch(got, /has\(|input\./);
});

test('apiTable rejects a non-empty raw condition without a human description', () => {
  assert.throws(() => apiTable({
    api_calls: [{
      api: 'UpdateThing',
      phase: 'operation',
      condition: 'mystery(input.name)',
      purpose: 'Update the thing.',
    }],
  }, english), /UpdateThing.*mystery\(input\.name\)/);
});

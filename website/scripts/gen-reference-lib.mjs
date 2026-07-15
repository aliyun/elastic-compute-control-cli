function escCell(text) {
  return String(text || '')
    .replace(/\r?\n/g, ' ')
    .replace(/\|/g, '\\|')
    .replace(/</g, '&lt;')
    .replace(/\{/g, '&#123;')
    .trim();
}

export function apiTable(action, t) {
  const calls = action.api_calls || [];
  if (!calls.length) return '';
  const rows = calls.map((call) => {
    const condition = call.condition_description;
    if (!condition) {
      throw new Error(`API ${call.api} is missing a human-readable description for condition ${call.condition || '<always>'}`);
    }
    let purpose = call.purpose || '';
    if (call.repeated) purpose = t.annotate(purpose, t.repeated);
    if (call.cached) purpose = t.annotate(purpose, t.cached);
    return `| \`${call.api}\` | ${escCell(condition)} | ${escCell(purpose)} |`;
  });
  return `${t.apiHeader}\n${rows.join('\n')}`;
}

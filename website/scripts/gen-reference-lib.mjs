import {
  resourceReferenceFilename,
  resourceReferenceLabel,
} from './reference-path.mjs';

export function generatedFrontMatter(fields) {
  return [
    '---',
    'generated: true',
    'generated_by: "website/scripts/gen-reference.mjs"',
    'generated_command: "make build && npm --prefix website run gen:reference"',
    ...fields,
    '---',
  ].join('\n');
}

export function escCell(text) {
  return String(text || '')
    .replace(/\r?\n/g, ' ')
    .replace(/\\/g, '\\\\')
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

export function productIndexPage(products, t) {
  const blocks = [
    generatedFrontMatter([
      `title: ${t.title}`,
      `description: ${JSON.stringify(t.description)}`,
    ]),
    `# ${t.title}`,
    t.intro,
  ];

  for (const product of products) {
    const productBlocks = [
      `## [${product.name.toUpperCase()}](../category/${product.name})`,
    ];
    if (product.description) productBlocks.push(product.description);
    productBlocks.push(`**${t.resourceCount(product.resources.length)}**`);
    const resourceRows = product.resources.map((resource) => {
      const label = resourceReferenceLabel(resource);
      if (!resource.description?.trim()) {
        throw new Error(`resource ${product.name}/${label} is missing description`);
      }
      const filename = resourceReferenceFilename(resource);
      const link = `[${label}](./resources/${product.name}/${filename})`;
      return `| ${link} | ${escCell(resource.description)} |`;
    });
    productBlocks.push([
      `| ${t.resourceHeader} | ${t.descriptionHeader} |`,
      '|---|---|',
      ...resourceRows,
    ].join('\n'));
    blocks.push(productBlocks.join('\n\n'));
  }

  return blocks.join('\n\n') + '\n';
}

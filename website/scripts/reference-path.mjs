export function resourceReferenceFilename(resource) {
  const stem = resource.parent ? `${resource.parent}-${resource.name}` : resource.name;
  return `${stem}.md`;
}

export function resourceReferenceLabel(resource) {
  return resource.parent ? `${resource.parent} ${resource.name}` : resource.name;
}

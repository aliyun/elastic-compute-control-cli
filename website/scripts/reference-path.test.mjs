import assert from 'node:assert/strict';
import test from 'node:test';

import {
  resourceReferenceFilename,
  resourceReferenceLabel,
} from './reference-path.mjs';

test('resourceReferenceFilename includes the parent for nested resources', () => {
  assert.equal(resourceReferenceFilename({name: 'instance'}), 'instance.md');
  assert.equal(
    resourceReferenceFilename({parent: 'policy', name: 'version'}),
    'policy-version.md',
  );
});

test('resourceReferenceLabel distinguishes nested resources', () => {
  assert.equal(resourceReferenceLabel({name: 'instance'}), 'instance');
  assert.equal(
    resourceReferenceLabel({parent: 'policy', name: 'version'}),
    'policy version',
  );
});

import { describe, expect, test } from 'vitest';
import { applyShortcutPlans, buildShortcutSplitPlans, clearSplitPreview, removeSplitPreviewGroup } from './services/materialSplit';
import type { ProductMaterialSKU } from './services/api';

const sku = (value: string, index: number): ProductMaterialSKU => ({
  material_sku_id: `${value}-${index}`,
  sku_type: 'source', source_sku_id: `${value}-${index}`,
  price_cent: 100, quantity: 1, enabled: true,
  properties: [{ name: '适用年龄', value }, { name: '颜色', value: `颜色${index}` }],
});

describe('material shortcut split', () => {
  test('automatically splits 270 SKUs into visible 252 and 18 groups', () => {
    const values = Array.from({ length: 15 }, (_, index) => `苹果${index + 5}`);
    const skus = values.flatMap(value => Array.from({ length: 18 }, (_, index) => sku(value, index)));
    const result = buildShortcutSplitPlans({ materialTitle: '手机壳', skus, occupiedIDs: new Set(), specification: '适用年龄', selectedValues: values, mode: 'merge' });
    expect(result.candidateCount).toBe(270);
    expect(result.plans.map(plan => plan.name)).toEqual(['合并组-1', '合并组-2']);
    expect(result.plans.map(plan => plan.skus.length)).toEqual([252, 18]);
    expect(new Set(result.plans.map(plan => plan.id)).size).toBe(2);
  });

  test('rejects duplicate stable IDs instead of silently overwriting assignments', () => {
    const duplicate = [sku('苹果16', 1), sku('苹果16', 1)];
    expect(() => buildShortcutSplitPlans({ materialTitle: '手机壳', skus: duplicate, occupiedIDs: new Set(), specification: '适用年龄', selectedValues: ['苹果16'], mode: 'merge' })).toThrow('稳定 ID 重复');
  });

  test('appends a second merged selection without replacing the first group', () => {
    const first = buildShortcutSplitPlans({ materialTitle: '手机壳', skus: [sku('苹果16', 1)], occupiedIDs: new Set(), specification: '适用年龄', selectedValues: ['苹果16'], mode: 'merge' });
    const preview1 = applyShortcutPlans({ existingGroups: [], existingAssignments: {}, plans: first.plans, replace: false, idPrefix: 'preview' });
    const second = buildShortcutSplitPlans({ materialTitle: '手机壳', skus: [sku('苹果15', 1)], occupiedIDs: new Set(), specification: '适用年龄', selectedValues: ['苹果15'], mode: 'merge' });
    const preview2 = applyShortcutPlans({ existingGroups: preview1.groups, existingAssignments: preview1.assignments, plans: second.plans, replace: false, idPrefix: 'preview' });
    expect(preview2.groups).toHaveLength(2);
    expect(Object.keys(preview2.assignments)).toHaveLength(2);
    expect(new Set(Object.values(preview2.assignments)).size).toBe(2);
  });

  test('deleting one group and generating again never reuses an active group identity', () => {
    const plans = buildShortcutSplitPlans({ materialTitle: '手机壳', skus: [sku('苹果16', 1), sku('苹果15', 1)], occupiedIDs: new Set(), specification: '适用年龄', selectedValues: ['苹果16', '苹果15'], mode: 'separate' });
    const initial = applyShortcutPlans({ existingGroups: [], existingAssignments: {}, plans: plans.plans, replace: false, idPrefix: 'preview' });
    const removed = removeSplitPreviewGroup({ groups: initial.groups, assignments: initial.assignments, groupID: initial.groups[0].id });
    const replacement = buildShortcutSplitPlans({ materialTitle: '手机壳', skus: [sku('苹果14', 1)], occupiedIDs: new Set(Object.keys(removed.assignments)), specification: '适用年龄', selectedValues: ['苹果14'], mode: 'merge' });
    const regenerated = applyShortcutPlans({ existingGroups: removed.groups, existingAssignments: removed.assignments, plans: replacement.plans, replace: false, idPrefix: 'preview' });
    expect(regenerated.groups).toHaveLength(2);
    expect(new Set(regenerated.groups.map(group => group.id)).size).toBe(2);
    expect(new Set(Object.values(regenerated.assignments)).size).toBe(2);
  });

  test('clearing preview removes every group and assignment', () => {
    const cleared = clearSplitPreview();
    expect(cleared.groups).toEqual([]);
    expect(cleared.assignments).toEqual({});
  });
});

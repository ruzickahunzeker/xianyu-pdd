import type { ProductMaterialSKU } from './api';

export type ShortcutSplitPlan = {
  id: string;
  name: string;
  title: string;
  skus: ProductMaterialSKU[];
};

export type ShortcutSplitResult = {
  candidateCount: number;
  plans: ShortcutSplitPlan[];
};

export type SplitPreviewGroup = { id: string; name: string; title: string };

export function applyShortcutPlans(input: {
  existingGroups: SplitPreviewGroup[];
  existingAssignments: Record<string, string>;
  plans: ShortcutSplitPlan[];
  replace: boolean;
  idPrefix: string;
}): { groups: SplitPreviewGroup[]; assignments: Record<string, string> } {
  const assignments = input.replace ? {} : { ...input.existingAssignments };
  const groups = input.replace ? [] : input.existingGroups.filter(group => Object.values(assignments).includes(group.id));
  const startIndex = groups.length;
  input.plans.forEach((plan, index) => {
    const id = `${input.idPrefix}-${startIndex + index + 1}`;
    groups.push({ id, name: plan.name, title: plan.title });
    for (const sku of plan.skus) if (sku.material_sku_id) assignments[sku.material_sku_id] = id;
  });
  return { groups, assignments };
}

const truncate = (value: string, max: number) => Array.from(value).slice(0, max).join('');

export function buildShortcutSplitPlans(input: {
  materialTitle: string;
  skus: ProductMaterialSKU[];
  occupiedIDs: Set<string>;
  specification: string;
  selectedValues: string[];
  mode: 'separate' | 'merge';
  maxPerGroup?: number;
}): ShortcutSplitResult {
  const maxPerGroup = input.maxPerGroup || 200;
  const selected = new Set(input.selectedValues);
  const seenStableIDs = new Set<string>();
  const candidates = input.skus.filter(sku => {
    const stableID = sku.material_sku_id;
    const value = sku.properties.find(property => property.name.trim() === input.specification)?.value.trim() || '';
    if (!stableID || input.occupiedIDs.has(stableID) || !selected.has(value)) return false;
    if (seenStableIDs.has(stableID)) throw new Error(`素材 SKU 稳定 ID 重复: ${stableID}`);
    seenStableIDs.add(stableID);
    return true;
  });
  const plans: ShortcutSplitPlan[] = [];
  const usedNames = new Set<string>();
  const appendChunks = (displayLabel: string, rows: ProductMaterialSKU[], merge: boolean) => {
    const chunkCount = Math.ceil(rows.length / maxPerGroup);
    for (let offset = 0; offset < rows.length; offset += maxPerGroup) {
      const part = Math.floor(offset / maxPerGroup) + 1;
      const suffix = chunkCount > 1 ? `-${part}` : '';
      const baseName = merge ? `合并组${suffix}` : `${displayLabel}${suffix}`;
      let name = truncate(baseName, 40);
      if (usedNames.has(name)) name = truncate(name, 36) + `-${plans.length + 1}`;
      usedNames.add(name);
      plans.push({
        id: `shortcut-${plans.length + 1}`,
        name,
        title: `${input.materialTitle} - ${displayLabel}${suffix}`,
        skus: rows.slice(offset, offset + maxPerGroup),
      });
    }
  };
  if (input.mode === 'merge') appendChunks(input.selectedValues.join('+'), candidates, true);
  else for (const value of input.selectedValues) {
    appendChunks(value, candidates.filter(sku => sku.properties.some(property => property.name.trim() === input.specification && property.value.trim() === value)), false);
  }
  return { candidateCount: candidates.length, plans };
}

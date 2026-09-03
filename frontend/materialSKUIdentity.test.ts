import { describe, expect, test } from 'vitest';
import { renameSKUProperties, sameSpecificationShape } from './components/ProductMaterials';
import type { ProductMaterialSKU } from './services/api';

describe('material SKU identity while renaming specifications', () => {
  test('keeps the source identity when a value is edited repeatedly', () => {
    const sku: ProductMaterialSKU = {
      material_sku_id: 'stable-1',
      sku_type: 'source',
      source_goods_id: '609274612506',
      source_sku_id: '1592206040899',
      source_properties: [{ name: '原规格', value: '原始名称' }],
      price_cent: 2799,
      quantity: 1000,
      enabled: true,
      properties: [{ name: '款式', value: 'BC车USB-C' }],
    };
    const before = [{ name: '款式', supportImage: false, values: [{ value: 'BC车USB-C' }] }];
    const editing = [{ name: '接口类型', supportImage: false, values: [{ value: 'BC车USB' }] }];
    const after = [{ name: '接口类型', supportImage: false, values: [{ value: 'BC车USB-C新名称' }] }];

    expect(sameSpecificationShape(before, editing)).toBe(true);
    const first = renameSKUProperties([sku], before, editing);
    const second = renameSKUProperties(first, editing, after);

    expect(second[0]).toMatchObject({
      material_sku_id: 'stable-1',
      sku_type: 'source',
      source_goods_id: '609274612506',
      source_sku_id: '1592206040899',
      source_properties: [{ name: '原规格', value: '原始名称' }],
      price_cent: 2799,
      quantity: 1000,
      properties: [{ name: '接口类型', value: 'BC车USB-C新名称' }],
    });
  });

  test('treats adding or removing a value as a structural change', () => {
    const before = [{ name: '款式', supportImage: false, values: [{ value: 'A' }] }];
    const after = [{ name: '款式', supportImage: false, values: [{ value: 'A' }, { value: 'B' }] }];
    expect(sameSpecificationShape(before, after)).toBe(false);
  });
});

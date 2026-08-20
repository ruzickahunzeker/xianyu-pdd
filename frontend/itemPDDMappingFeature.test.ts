import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, test } from 'vitest';

const source = (path: string) => readFileSync(resolve(__dirname, path), 'utf8');

describe('item PDD SKU mapping UI', () => {
  test('renders the mapping picker above the item detail modal and exposes loading failures', () => {
    const itemList = source('components/ItemList.tsx');
    expect(itemList).toContain('style={{zIndex:10000}}');
    expect(itemList).toContain('正在加载采集商品…');
    expect(itemList).toContain('setMappingProductsError');
    expect(itemList).not.toContain('style={{zIndex:120}}');
  });

  test('opens collected products with the captured full URL and keeps a canonical fallback', () => {
    const collected = source('components/PDDCollectedProducts.tsx');
    expect(collected).toContain('https://mobile.pinduoduo.com/goods.html?goods_id=${encodeURIComponent(product.goods_id)}');
    expect(collected).toContain('(?:pinduoduo|yangkeduo)');
  });

  test('publishes generated PDD matrix placeholders with zero inventory', () => {
    const materials = source('components/ProductMaterials.tsx');
    expect(materials).toContain('quantity: 0, enabled: true, properties');
    expect(materials).toContain("current.source_type === 'pdd' && !sku.source_sku_id");
  });

  test('supports keyboard navigation and selection in collected media preview', () => {
    const materials = source('components/ProductMaterials.tsx');
    expect(materials).toContain("event.key === 'ArrowLeft'");
    expect(materials).toContain("event.key === 'ArrowRight'");
    expect(materials).toContain("event.key === 'Enter' || event.key === ' '");
    expect(materials).toContain("event.key === 'Escape'");
    expect(materials).toContain('toggleMediaSelection(mediaPreview.key)');
    expect(materials).toContain('pickerChoices[(index + direction + pickerChoices.length) % pickerChoices.length]');
  });
});

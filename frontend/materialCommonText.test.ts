import { describe, expect, it } from 'vitest';
import { findCommonSpecificationTexts, removeCommonSpecificationText } from './components/ProductMaterials';

const specifications = [{
  name: '尺寸',
  supportImage: false,
  values: [
    '进口电芯【1.0米】全兼容不弹窗',
    '进口电芯【1.5米】全兼容不弹窗',
    '进口电芯【2.0米】全兼容不弹窗',
    '进口电芯【0.3米】全兼容不弹窗',
  ].map(value => ({ value })),
}];

describe('material specification common text', () => {
  it('lists only maximal text fragments shared by every value', () => {
    expect(findCommonSpecificationTexts(specifications).map(item => item.text)).toEqual([
      '进口电芯',
      '全兼容不弹窗',
    ]);
  });

  it('removes one selected fragment without changing the other fragment', () => {
    const next = removeCommonSpecificationText(specifications, '尺寸', '进口电芯');
    expect(next[0].values.map(item => item.value)).toEqual([
      '【1.0米】全兼容不弹窗',
      '【1.5米】全兼容不弹窗',
      '【2.0米】全兼容不弹窗',
      '【0.3米】全兼容不弹窗',
    ]);
  });

  it('does not report text that is only shared by some values', () => {
    const candidates = findCommonSpecificationTexts([{
      name: '颜色', supportImage: false, values: [
        { value: '发三件【240W秒充】A-C：适用华为安卓' },
        { value: '发两件【240W秒充】A-C：适用华为安卓' },
        { value: '发一件【240W秒充】C-C：适用苹果华为' },
        { value: '发一件【055W秒充】A-C：适用华为安卓' },
      ],
    }]);
    expect(candidates.map(item => item.text)).not.toContain('适用华为安卓');
    expect(candidates.map(item => item.text)).not.toContain('240W秒充');
  });
});

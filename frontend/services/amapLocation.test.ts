import {describe, expect, it} from 'vitest';
import {amapPOIToPublishLocation} from './amapLocation';

describe('amapPOIToPublishLocation', () => {
  it('maps a complete POI to the Xianyu publish location contract', () => {
    expect(amapPOIToPublishLocation({
      id: 'B0TEST', name: '测试地点', adname: '福田区', cityname: '深圳市', adcode: '440304', pname: '广东省',
      location: {lng: 114.085947, lat: 22.547},
    })).toEqual({
      area: '福田区', city: '深圳市', division_id: '440304', longitude: 114.085947, latitude: 22.547,
      poi_id: 'B0TEST', poi_name: '测试地点', province: '广东省',
    });
  });

  it('rejects missing administrative data and invalid coordinates', () => {
    expect(amapPOIToPublishLocation({id: 'x', name: 'x', location: {lng: 114, lat: 22}})).toBeNull();
    expect(amapPOIToPublishLocation({id: 'x', name: 'x', adname: '区', cityname: '市', adcode: '1', pname: '省', location: {lng: 181, lat: 22}})).toBeNull();
  });
});

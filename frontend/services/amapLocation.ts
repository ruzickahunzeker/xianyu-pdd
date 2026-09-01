import type { PublishLocation } from './api';

const AMAP_SCRIPT_ID = 'xianyu-pdd-amap-js-api';
const DEFAULT_AMAP_JS_KEY = 'c9b68d4ce9a2a97f22a4a439404488ca';

export interface AMapPOI {
  id?: string;
  name?: string;
  adname?: string;
  cityname?: string;
  adcode?: string | number;
  pname?: string;
  location?: {lng: number; lat: number};
}

interface AMapAPI {
  PlaceSearch: new (options: {extensions: 'all'; pageSize: number}) => {
    search(keyword: string, callback: (status: string, result: {poiList?: {pois?: AMapPOI[]}}) => void): void;
    searchNearBy(keyword: string, center: [number, number], radius: number,
      callback: (status: string, result: {poiList?: {pois?: AMapPOI[]}}) => void): void;
  };
}

declare global {
  interface Window {
    AMap?: AMapAPI;
    __xianyuPddAmapLoaded?: () => void;
  }
  interface ImportMetaEnv { readonly VITE_AMAP_JS_KEY?: string; }
  interface ImportMeta { readonly env: ImportMetaEnv; }
}

let amapLoadPromise: Promise<AMapAPI> | null = null;

const configuredAmapKey = (): string => import.meta.env.VITE_AMAP_JS_KEY?.trim() || DEFAULT_AMAP_JS_KEY;

const loadAMap = (): Promise<AMapAPI> => {
  if (window.AMap) return Promise.resolve(window.AMap);
  if (amapLoadPromise) return amapLoadPromise;
  amapLoadPromise = new Promise<AMapAPI>((resolve, reject) => {
    const existing = document.getElementById(AMAP_SCRIPT_ID) as HTMLScriptElement | null;
    const script = existing || document.createElement('script');
    const cleanup = () => {
      window.__xianyuPddAmapLoaded = undefined;
      window.clearTimeout(timeout);
    };
    const finish = () => {
      cleanup();
      window.AMap ? resolve(window.AMap) : reject(new Error('高德地图 API 加载完成但未找到 AMap 对象'));
    };
    const timeout = window.setTimeout(() => {
      cleanup();
      reject(new Error('高德地图 API 加载超时，请检查网络或 VITE_AMAP_JS_KEY 配置'));
    }, 15_000);
    window.__xianyuPddAmapLoaded = finish;
    script.id = AMAP_SCRIPT_ID;
    script.async = true;
    script.src = `https://webapi.amap.com/maps?v=2.0&key=${encodeURIComponent(configuredAmapKey())}&plugin=AMap.PlaceSearch&callback=__xianyuPddAmapLoaded`;
    script.onerror = () => {
      cleanup();
      reject(new Error('高德地图 API 加载失败，请检查网络或 VITE_AMAP_JS_KEY 配置'));
    };
    if (!existing) document.head.appendChild(script);
  }).catch(error => {
    amapLoadPromise = null;
    throw error;
  });
  return amapLoadPromise;
};

const mapSearchResult = (result: {poiList?: {pois?: AMapPOI[]}}): PublishLocation[] =>
  (result?.poiList?.pois || []).map(amapPOIToPublishLocation).filter((item): item is PublishLocation => item !== null);

export const searchPublishLocations = async (keyword: string): Promise<PublishLocation[]> => {
  const query = keyword.trim();
  if (query.length < 2) throw new Error('请输入至少 2 个字的省市区、商圈或地点名称');
  const amap = await loadAMap();
  return new Promise<PublishLocation[]>((resolve, reject) => {
    const search = new amap.PlaceSearch({extensions: 'all', pageSize: 20});
    search.search(query, (status, result) => {
      if (status === 'no_data') return resolve([]);
      if (status !== 'complete') return reject(new Error('高德地图地点搜索失败，请稍后重试'));
      resolve(mapSearchResult(result));
    });
  });
};

const validCoordinate = (value: number): boolean => Number.isFinite(value) && value !== 0;

export const amapPOIToPublishLocation = (poi: AMapPOI): PublishLocation | null => {
  const longitude = Number(poi.location?.lng);
  const latitude = Number(poi.location?.lat);
  const location: PublishLocation = {
    area: String(poi.adname || '').trim(),
    city: String(poi.cityname || '').trim(),
    division_id: String(poi.adcode || '').trim(),
    longitude,
    latitude,
    poi_id: String(poi.id || '').trim(),
    poi_name: String(poi.name || '').trim(),
    province: String(poi.pname || '').trim(),
  };
  if (!location.division_id || !location.province || !location.city || !location.poi_id || !location.poi_name) return null;
  if (!validCoordinate(longitude) || !validCoordinate(latitude) || longitude < -180 || longitude > 180 || latitude < -90 || latitude > 90) return null;
  return location;
};

export const getPublishLocations = async (longitude: number, latitude: number): Promise<PublishLocation[]> => {
  if (!validCoordinate(longitude) || !validCoordinate(latitude) || longitude < -180 || longitude > 180 || latitude < -90 || latitude > 90) {
    throw new Error('经纬度无效');
  }
  const amap = await loadAMap();
  return new Promise<PublishLocation[]>((resolve, reject) => {
    const search = new amap.PlaceSearch({extensions: 'all', pageSize: 10});
    search.searchNearBy('', [longitude, latitude], 1_000, (status, result) => {
      if (status === 'no_data') return resolve([]);
      if (status !== 'complete') return reject(new Error('高德地图附近地址查询失败，请稍后重试'));
      resolve(mapSearchResult(result));
    });
  });
};

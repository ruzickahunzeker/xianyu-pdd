import { get, post, put, del, postForm, type RequestControlOptions } from '../request';
import {
  LoginResponse, AccountDetail, Order, PaginatedResponse,
  AdminStats, DashboardStats, Card, SystemSettings, ApiResponse, OrderAnalytics,
  Item, AIReplySettings, ShippingRule, ReplyRule, DefaultReply, AutomationAction, AutomationTriggerType,
  NotificationChannel, NotificationEventType, PDDAccountConfig
	, AccountTaskSettings, AccountTaskSummary, ChatSession, ChatMessage
} from '../types';
import { formatLocalDate } from '../dateRange';

const normalizeSettings = (settings: Record<string, any>): SystemSettings => {
  const out: Record<string, any> = { ...settings };
  if ('renewal_log_retention_days' in out) {
    const parsed = Number(out.renewal_log_retention_days);
    out.renewal_log_retention_days = Number.isFinite(parsed) ? parsed : 10;
  }
  if ('order_sync_interval_minutes' in out) {
    const parsed = Number(out.order_sync_interval_minutes);
    out.order_sync_interval_minutes = Number.isFinite(parsed) ? parsed : 10;
  }
  if ('order_sync_enabled' in out) {
    out.order_sync_enabled = String(out.order_sync_enabled).toLowerCase() === 'true';
  }
  return out as SystemSettings;
};

// Auth
export const login = async (data: { username?: string; password?: string; email?: string; verification_code?: string }): Promise<LoginResponse> => {
  return post('/login', data, { skipAuthLogout: true });
};

export const initializeAdmin = async (password: string): Promise<LoginResponse> => {
  return post('/initialize', { password }, { skipAuthLogout: true });
};

export const verifySession = async (): Promise<{ authenticated: boolean; initialized?: boolean; user_id?: number; username?: string; is_admin?: boolean }> => {
  return get('/verify');
};

export const logout = async (): Promise<ApiResponse> => {
  return post('/logout', {});
};

export const changePassword = async (currentPassword: string, newPassword: string): Promise<ApiResponse> => {
  return post('/change-password', { current_password: currentPassword, new_password: newPassword });
};

export const updateLoginCredentials = async (data: {
  current_password: string;
  new_username: string;
  new_password?: string;
}): Promise<ApiResponse & { requires_relogin?: boolean }> => {
  return put('/account/credentials', data);
};

export interface DatabaseBackup {
  id: string;
  filename: string;
  dialect: string;
  created_at: number;
  size_bytes: number;
  sha256: string;
  mapping_rows: number;
  data_key_configured: boolean;
  verified: boolean;
}

export const getDatabaseBackups = (): Promise<{backups: DatabaseBackup[]; dialect: string}> => get('/api/backups');
export const createDatabaseBackup = (): Promise<DatabaseBackup> => post('/api/backups', {}, {timeoutMs: 30 * 60_000});
export const databaseBackupDownloadURL = (id: string): string => `/api/backups/${encodeURIComponent(id)}/download`;

// Accounts
export const addAccount = async (id: string, value: string, loginMethod?: string): Promise<ApiResponse> => {
  return post('/cookies', { id, value, login_method: loginMethod });
};

const accountAvatarURL = (item: any, version: string): string => {
  const raw = item.avatar_url || '';
  if (!raw) return '';

  try {
    const url = new URL(raw, window.location.origin);
    if (url.hostname.endsWith('alicdn.com')) {
      url.searchParams.set('_v', version);
    }
    return url.toString();
  } catch {
    return raw;
  }
};

export const getAccountDetails = async (options?: RequestControlOptions): Promise<AccountDetail[]> => {
  const data = await get<any[]>('/cookies/details', undefined, options);
  const avatarVersion = Date.now().toString();
  return data.map(item => ({
    id: item.id,
    value: '',
    cookie: '',
    enabled: item.enabled,
    auto_confirm: item.auto_confirm,
    remark: item.remark,
    note: item.remark,
    pause_duration: item.pause_duration,
    paused_until: Number(item.paused_until || 0),
    paused: item.paused === true,
    username: item.username || '',
    login_password: '',
    show_browser: item.show_browser === true || item.show_browser === 1 || item.show_browser === '1' || item.show_browser === 'true',
    nickname: item.nickname || item.remark || `账号 ${item.id.substring(0,6)}`,
    avatar_url: accountAvatarURL(item, avatarVersion),
    profile_error: item.profile_error || '',
    ai_enabled: false,
		auto_rate_enabled: item.auto_rate_enabled === true || item.auto_rate_enabled === 1,
		rate_content: item.rate_content || '不错的买家，交易愉快',
		auto_polish_enabled: item.auto_polish_enabled === true || item.auto_polish_enabled === 1,
		polish_time: item.polish_time || '03:00',
		last_rate_scan_at: Number(item.last_rate_scan_at || 0),
		last_polish_date: item.last_polish_date || '',
		last_polish_at: Number(item.last_polish_at || 0),
  }));
};

export const getAccountTaskSettings = async (id: string): Promise<AccountTaskSettings> =>
	get(`/api/account-tasks/${id}`);

export const updateAccountTaskSettings = async (id: string, settings: AccountTaskSettings): Promise<AccountTaskSettings> =>
	put(`/api/account-tasks/${id}`, settings);

export const runAccountTask = async (id: string, taskType: 'auto_rate' | 'auto_polish'): Promise<{success: boolean; summary: AccountTaskSummary}> =>
	post(`/api/account-tasks/${id}/run`, { task_type: taskType }, { timeoutMs: 120_000 });

export interface ChatSessionPage { sessions: ChatSession[]; has_more: boolean; next_cursor?: number }

export const getChatSessionPage = async (accountId: string, cursor?: number, options?: RequestControlOptions, refresh = false): Promise<ChatSessionPage> => {
	const result = await get<ChatSessionPage>('/api/chat/sessions', { account_id: accountId, cursor, refresh: refresh ? 1 : undefined },
		refresh ? { timeoutMs: 60_000, ...options } : options);
	return { sessions: result.sessions || [], has_more: result.has_more === true, next_cursor: result.next_cursor };
};

export const getChatSessions = async (accountId: string, options?: RequestControlOptions): Promise<ChatSession[]> =>
	(await getChatSessionPage(accountId, undefined, options)).sessions;

export interface ChatMessagePage {
	messages: ChatMessage[];
	has_more: boolean;
	next_cursor?: number;
	session?: ChatSession;
}

export const getChatMessagePage = async (accountId: string, chatId: string, cursor?: number, beforeId?: number, options?: RequestControlOptions): Promise<ChatMessagePage> => {
	const result = await get<ChatMessagePage>('/api/chat/messages', {
		account_id: accountId, chat_id: chatId, cursor, before_id: beforeId,
	}, options);
	return { messages: result.messages || [], has_more: result.has_more === true, next_cursor: result.next_cursor, session: result.session };
};

export const getChatMessages = async (accountId: string, chatId: string, beforeId?: number, options?: RequestControlOptions): Promise<ChatMessage[]> =>
	(await getChatMessagePage(accountId, chatId, undefined, beforeId, options)).messages;

export const sendChatMessage = async (input: {
	account_id: string; chat_id: string; buyer_id: string; buyer_name?: string;
	item_id?: string; item_title?: string; text: string;
}): Promise<{message: ChatMessage}> => post('/api/chat/messages', input);

export const sendChatImage = async (input: {
	account_id: string; chat_id: string; buyer_id: string; buyer_name?: string;
	buyer_avatar_url?: string; item_id?: string; item_title?: string; image: File;
}): Promise<{message: ChatMessage}> => {
	const form = new FormData();
	Object.entries(input).forEach(([key, value]) => form.append(key, value));
	return postForm('/api/chat/images', form, { timeoutMs: 120_000 });
};

export const markChatRead = async (accountId: string, chatId: string): Promise<ApiResponse> =>
	post('/api/chat/read', { account_id: accountId, chat_id: chatId });

export interface AccountRuntimeStatus {
  state: NonNullable<AccountDetail['runtime_state']>;
  message?: string;
  connected: boolean;
  failures: number;
  updated_at: string;
}

export const getAccountRuntimeStatuses = async (options?: RequestControlOptions): Promise<Record<string, AccountRuntimeStatus>> => {
  return get('/cookies/runtime-status', undefined, options);
};

export const generateQRLogin = async (options?: RequestControlOptions): Promise<{ success: boolean; session_id?: string; qr_code_url?: string }> => {
  return post('/qr-login/generate', undefined, options);
};

export const checkQRLoginStatus = async (sessionId: string, signal?: AbortSignal): Promise<any> => {
  return get(`/qr-login/check/${sessionId}`, undefined, { signal, timeoutMs: 10_000 });
};

export const completeQRVerification = async (
  sessionId: string,
  targetAccountId?: string,
): Promise<{
  success: boolean;
  account_id?: string;
  scanned_account_id?: string;
  message?: string;
}> => {
  return post(`/qr-login/complete-verification/${sessionId}`, {
    target_account_id: targetAccountId || '',
  });
};

export const updateAccountStatus = async (id: string, enabled: boolean): Promise<any> => {
  return put(`/cookies/${id}/status`, { enabled });
};

export const deleteAccount = async (id: string): Promise<any> => {
  return del(`/cookies/${id}`);
};

export const updateAccountRemark = async (id: string, remark: string): Promise<any> => {
  return put(`/cookies/${id}/remark`, { remark });
};

export const updateAccountAutoConfirm = async (id: string, autoConfirm: boolean): Promise<any> => {
  return put(`/cookies/${id}/auto-confirm`, { auto_confirm: autoConfirm });
};

export const updateAccountPauseDuration = async (id: string, pauseDuration: number): Promise<any> => {
  return put(`/cookies/${id}/pause-duration`, { pause_duration: pauseDuration });
};

export const updateAccountCookie = async (id: string, value: string, loginMethod?: string): Promise<any> => {
  return put(`/cookies/${id}`, { id, value, login_method: loginMethod });
};

export interface AccountSettingsUpdate {
  cookie?: string;
  remark?: string;
  auto_confirm?: boolean;
  pause_duration?: number;
  username?: string;
  login_password?: string;
  clear_password?: boolean;
  show_browser?: boolean;
  channel_ids?: number[];
}

export const updateAccountSettings = async (id: string, data: AccountSettingsUpdate): Promise<ApiResponse> => {
  return put(`/cookies/${id}/settings`, data);
};

export interface LongLoginSettings {
  can_open_long_login: boolean;
  enabled: boolean;
}

export const getLongLoginSettings = async (id: string): Promise<LongLoginSettings> => {
  return get(`/cookies/${id}/long-login`);
};

export const setLongLoginSettings = async (id: string, enabled: boolean): Promise<LongLoginSettings> => {
  return put(`/cookies/${id}/long-login`, { enabled });
};

export interface PasswordLoginStartResponse {
  success: boolean;
  session_id?: string;
  status?: 'processing' | 'failed';
  message?: string;
}

export interface PasswordLoginStatusResponse {
  status: 'processing' | 'success' | 'failed' | 'verification_required' | 'not_found' | 'error';
  message?: string;
  account_id?: string;
  is_new_account?: boolean;
  cookie_count?: number;
  verification_url?: string;
  screenshot_path?: string;
  qr_code_url?: string;
  error?: string;
  reason?: string;
}

export const passwordLogin = async (data: {
  account_id: string;
  account: string;
  password: string;
  show_browser?: boolean;
}): Promise<PasswordLoginStartResponse> => {
  return post('/password-login', data);
};

export const checkPasswordLoginStatus = async (sessionId: string, signal?: AbortSignal): Promise<PasswordLoginStatusResponse> => {
  return get(`/password-login/check/${sessionId}`, undefined, { signal, timeoutMs: 10_000 });
};

export const cancelPasswordLogin = async (sessionId: string): Promise<ApiResponse> => {
  return del(`/password-login/cancel/${sessionId}`);
};

export const refreshAccountProfile = async (id: string): Promise<any> => {
  return post(`/cookies/${id}/refresh-profile`, {});
};

export const updateAccountLoginInfo = async (id: string, data: {
  username?: string;
  login_password?: string;
  clear_password?: boolean;
  show_browser?: boolean;
}): Promise<any> => {
  return put(`/cookies/${id}/login-info`, data);
};

export const getAllAISettings = async (options?: RequestControlOptions): Promise<Record<string, AIReplySettings>> => {
  return get('/ai-reply-settings', undefined, options);
};

// Orders
const normalizeOrderStatus = (value: unknown): Order['status'] => {
  const status = String(value || '');
  if (status === 'paid') return 'pending_ship';
  return ['processing', 'pending_ship', 'shipped', 'completed', 'cancelled', 'refunding'].includes(status)
    ? status as Order['status']
    : 'unknown';
};

export const getOrders = async (
  cookieId?: string,
  status?: string,
  page: number = 1,
  pageSize: number = 20,
  search?: string,
): Promise<PaginatedResponse<Order>> => {
  const params: any = { page, page_size: pageSize };
  if (cookieId) params.cookie_id = cookieId;
  if (status && status !== 'all') params.status = status;
  if (search?.trim()) params.search = search.trim();

  const res = await get<any>('/api/orders', params);

  // Handle backend response variations
  const rawOrders = res.orders || res.data || [];
  const orders = rawOrders.map((item: any) => ({
    ...item,
    id: item.id || item.order_id,
    status: normalizeOrderStatus(item.status || item.order_status),
    quantity: Number(item.quantity || 1),
  }));
  return {
    success: true,
    data: orders,
    total: res.total || orders.length,
    page: res.page || page,
    page_size: res.page_size || pageSize,
    total_pages: res.total_pages || 1
  };
};

export const getOrderDetail = async (orderId: string): Promise<{ success: boolean; data?: Order }> => {
  const result = await get<{ order?: Order; data?: Order }>(`/api/orders/${orderId}`);
  return {
    success: true,
    data: result.order || result.data
  };
};

export const updateOrder = async (orderId: string, data: Partial<Order>): Promise<ApiResponse> => {
  return put(`/api/orders/${orderId}`, data);
};

export const deleteOrder = async (orderId: string): Promise<ApiResponse> => {
  return del(`/api/orders/${orderId}`);
};

export const syncOrders = async (cookieId?: string, status?: string): Promise<any> => {
  const formData = new FormData();
  if (cookieId) formData.append('cookie_id', cookieId);
  if (status) formData.append('status', status);

	return postForm('/api/orders/refresh', formData);
};

export interface OrderSyncRun {
	trigger_type: 'manual' | 'scheduled';
	status: 'success' | 'partial' | 'failed';
	started_at: number;
	finished_at: number;
	discovered: number;
	updated: number;
	soft_deleted: number;
	fulfillment_updated: number;
	failed: number;
	error_message: string;
}

export interface OrderSyncStatus {
	enabled: boolean;
	interval_minutes: number;
	running: boolean;
	next_run_at: number;
	last_run: OrderSyncRun | null;
}

export const getOrderSyncStatus = async (): Promise<OrderSyncStatus> => get('/api/orders/sync-status');

export const syncSingleOrder = async (orderId: string): Promise<any> => {
  return post(`/api/orders/${orderId}/refresh`);
};

export const manualShipOrder = async (orderIds: string[], shipMode: 'status_only' | 'full_delivery'): Promise<any> => {
    return post('/api/orders/manual-ship', {
        order_ids: orderIds,
        ship_mode: shipMode,
    });
}

export const importOrders = async (data: Partial<Order>[] | FormData): Promise<any> => {
	const isFormData = data instanceof FormData;
	return isFormData ? postForm('/api/orders/import', data) : post('/api/orders/import', data);
}

// Stats
export const getAdminStats = async (): Promise<AdminStats> => {
  return get('/admin/stats');
};

export interface FulfillmentOrder {
  order_id:string; cookie_id:string; item_id:string; spec_name:string; spec_value:string;
  receiver_name:string; receiver_phone:string; receiver_address:string; receiver_city:string;
  material_id:number; material_sku_id:string; source_goods_id:string; source_sku_id:string; xianyu_sku_id:string;
  mapping_status:'pending'|'mapped'|'unmapped'|'ambiguous'|string; pdd_ordered:boolean; pdd_paid:boolean; pdd_paid_at:number; pdd_paid_source:string; pdd_order_id:string;
  pdd_order?: { order_id?:string; group_order_id?:string; goods_id?:string; sku_id?:string; quantity?:number; amount_cent?:number; order_time?:number; payment_deadline?:number; receiver_name?:string; province?:string; city?:string; district?:string; detail_address?:string };
  pdd_shipped:boolean; logistics_company:string; tracking_number:string; xianyu_shipped:boolean; reminded:boolean;
  fulfillment_exempt:boolean; reminder_exempt:boolean; manual_modified_at:number; history_repaired_at:number;
  phone_restore_due_at:number; address_match_status:string; last_error:string; purchase_requested_at:number; updated_at:number;
}
export interface FulfillmentOrderFilters { pdd_ordered?:boolean; pdd_paid?:boolean; pdd_shipped?:boolean; xianyu_shipped?:boolean; reminded?:boolean; mapping_status?:string; }
export type FulfillmentOrderPatch = Partial<Pick<FulfillmentOrder,'pdd_ordered'|'pdd_paid'|'pdd_order_id'|'pdd_shipped'|'logistics_company'|'tracking_number'|'xianyu_shipped'|'reminded'>>;
export const reconcileFulfillments = ():Promise<{success:boolean;updated:number}> => post('/api/fulfillment/reconcile',{});
export const getFulfillmentOrders = (filters:FulfillmentOrderFilters = {}):Promise<FulfillmentOrder[]> => get('/api/fulfillment/orders',{...filters});
export const updateFulfillmentOrder = (orderId:string,data:FulfillmentOrderPatch):Promise<ApiResponse> => put(`/api/fulfillment/orders/${encodeURIComponent(orderId)}`,data);
export const updateFulfillment = updateFulfillmentOrder;
export const requestFulfillmentPurchase = (orderId:string):Promise<{success:boolean;order_id:string;status:string;purchase_requested_at:number}> => post(`/api/fulfillment/orders/${encodeURIComponent(orderId)}/purchase-request`,{});
export const previewFulfillmentAddress = (orderId:string):Promise<Record<string,unknown>> => post(`/api/fulfillment/orders/${encodeURIComponent(orderId)}/address-preview`,{});
export interface FulfillmentHistoryRepairPreview { eligible:number; active_excluded:number; manual_excluded:number; pdd_excluded:number; }
export const previewFulfillmentHistoryRepair = ():Promise<FulfillmentHistoryRepairPreview> => get('/api/fulfillment/history-repair/preview');
export const repairFulfillmentHistory = ():Promise<{success:boolean;updated:number}> => post('/api/fulfillment/history-repair',{});
export interface PDDPurchaseTask {
  id:string; order_id:string; pdd_account_id:string; attempt:number; status:string; worker_id:string; lease_expires_at:number;
  source_goods_id:string; source_sku_id:string; quantity:number; xianyu_amount_cent:number; pdd_order_id:string; last_error:string;
  created_at:number; updated_at:number;
}
export const getPDDPurchaseTasks = ():Promise<PDDPurchaseTask[]> => get('/api/fulfillment/purchase-tasks');
export const confirmPDDPurchasePayment = (taskId:string,pddOrderId:string):Promise<ApiResponse> => post(`/api/fulfillment/purchase-tasks/${encodeURIComponent(taskId)}/confirm-payment`,{pdd_order_id:pddOrderId});
export const confirmUnknownPurchaseCancelled = (taskId:string):Promise<ApiResponse> => post(`/api/fulfillment/purchase-tasks/${encodeURIComponent(taskId)}/confirm-cancelled`,{});
export interface FulfillmentException { id:string; order_id:string; task_id:string; event_type:string; summary:string; status:string; notification_status:string; created_at:number; resolved_at:number; read_at:number; }
export const getFulfillmentExceptions = ():Promise<FulfillmentException[]> => get('/api/fulfillment/exceptions');
export const readFulfillmentExceptions = ():Promise<{success:boolean;updated:number;read_at:number}> => put('/api/fulfillment/exceptions/read',{});
export const clearFulfillmentExceptions = (scope:'all'|'resolved'='all'):Promise<{success:boolean;deleted:number}> => del(`/api/fulfillment/exceptions?scope=${scope}`);
export const resolveFulfillmentException = (eventId:string):Promise<ApiResponse> => put(`/api/fulfillment/exceptions/${encodeURIComponent(eventId)}/resolve`,{});
export interface ShippingPrecheck { ready:boolean; problems:string[]; order_id:string; pdd_order_id:string; logistics_company:string; logistics_company_code:string; tracking_number:string; shipping_status:string; }
export const shippingPrecheck = (orderId:string):Promise<ShippingPrecheck> => post(`/api/fulfillment/orders/${encodeURIComponent(orderId)}/shipping-precheck`,{});
export const submitPhysicalShipment = async (orderId:string,idempotencyKey:string):Promise<ApiResponse & {status?:string;error?:string;replayed?:boolean}> => {
  const response=await fetch(`/api/fulfillment/orders/${encodeURIComponent(orderId)}/ship`,{method:'POST',credentials:'include',headers:{'Content-Type':'application/json','Idempotency-Key':idempotencyKey},body:'{}'});
  const data=await response.json().catch(()=>({})); if(!response.ok) throw new Error(data.error||data.detail||`HTTP ${response.status}`); return data;
};
export interface ShippingAddressOption { contact_id:number; area_id:number; contact_name:string; mobile_phone:string; province_name:string; city_name:string; district_name:string; detail_address:string; platform_default:boolean; last_synced_at:number; }
export interface ShippingAccountConfig { cookie_id:string; remark:string; address_id:number; address_summary:string; verified_at:number; addresses:ShippingAddressOption[]; }
export const getShippingAccounts = ():Promise<ShippingAccountConfig[]> => get('/api/fulfillment/shipping-accounts');
export const saveShippingAccount = (cookieId:string,addressId:number,addressSummary=''):Promise<ApiResponse> => put(`/api/fulfillment/shipping-accounts/${encodeURIComponent(cookieId)}`,{address_id:addressId,address_summary:addressSummary});
export const syncShippingAccountAddresses = (cookieId:string):Promise<{success:boolean;count:number;selected_address_id:number}> => post(`/api/fulfillment/shipping-accounts/${encodeURIComponent(cookieId)}/sync`,{});

export interface PDDMessageTask { id:string;pdd_account_id:string;goods_id:string;sku_id:string;mall_sn:string;task_type:string;message:string;business_id:string;xianyu_order_id:string;pdd_order_id:string;send_mode:string;status:string;attempts:number;sent_at:number;verified_at:number;last_error:string;created_at:number; }
export interface PDDMessageInput { pdd_account_id?:string;goods_id:string;sku_id?:string;mall_sn?:string;task_type:string;business_id?:string;xianyu_order_id?:string;pdd_order_id?:string;message:string;send_mode:'manual_confirm';metadata?:Record<string,unknown>; }
export const getPDDMessages = (status=''):Promise<PDDMessageTask[]> => get(`/api/pdd/messages${status?`?status=${encodeURIComponent(status)}`:''}`);
export const createPDDMessage = async (input:PDDMessageInput,idempotencyKey:string):Promise<PDDMessageTask> => { const response=await fetch('/api/pdd/messages',{method:'POST',credentials:'include',headers:{'Content-Type':'application/json','Idempotency-Key':idempotencyKey},body:JSON.stringify(input)});const data=await response.json().catch(()=>({}));if(!response.ok)throw new Error(data.error||data.detail||`HTTP ${response.status}`);return data; };
export const confirmPDDMessage = (id:string):Promise<ApiResponse> => post(`/api/pdd/messages/${encodeURIComponent(id)}/confirm`,{});
export const cancelPDDMessage = (id:string):Promise<ApiResponse> => post(`/api/pdd/messages/${encodeURIComponent(id)}/cancel`,{});
export const retryPDDMessage = (id:string):Promise<ApiResponse> => post(`/api/pdd/messages/${encodeURIComponent(id)}/retry`,{});
export interface PDDChatConversation { id:string;pdd_account_id:string;mall_sn:string;mall_id:string;goods_id:string;pdd_order_id:string;page_url:string;last_message_id:string;last_sync_at:number;titan_wakeup_at:number;status:string; }
export interface PDDChatMessage { id:string;conversation_id:string;platform_message_id:string;direction:'incoming'|'outgoing'|'unknown';message_type:string;content:string;platform_created_at:number;created_at:number;mall_sn:string;mall_id:string;goods_id:string;pdd_order_id:string; }
export const getPDDChatConversations = ():Promise<PDDChatConversation[]> => get('/api/pdd/chat/conversations');
export const getPDDChatMessages = (conversationId=''):Promise<PDDChatMessage[]> => get(`/api/pdd/chat/messages${conversationId?`?conversation_id=${encodeURIComponent(conversationId)}`:''}`);

export const getDashboardStats = async (): Promise<DashboardStats> => {
  return get('/dashboard/stats');
};

export const getOrderAnalytics = async (daysOrParams: number | {start_date: string; end_date: string} = 7): Promise<OrderAnalytics> => {
    let params: {start_date: string; end_date: string};

    if (typeof daysOrParams === 'number') {
        const endDate = new Date();
        const startDate = new Date();
        startDate.setDate(startDate.getDate() - daysOrParams);
        params = {
            start_date: formatLocalDate(startDate),
            end_date: formatLocalDate(endDate)
        };
    } else {
        params = daysOrParams;
    }

    return get('/analytics/orders', {
        ...params,
        timezone_offset_minutes: -new Date().getTimezoneOffset(),
    });
}

export interface ValidOrdersResult {
    orders: Order[];
    total: number;
    truncated: boolean;
}

export const getValidOrders = async (dateRange: {start_date: string; end_date: string}): Promise<ValidOrdersResult> => {
    const res = await get<any>('/analytics/orders/valid', {
        start_date: dateRange.start_date,
        end_date: dateRange.end_date,
        timezone_offset_minutes: -new Date().getTimezoneOffset(),
    });
    const orders = Array.isArray(res) ? res : (res.orders || []);
    const normalized = orders.map((order: any) => ({
        ...order,
        id: order.id || order.order_id,
        status: normalizeOrderStatus(order.status || order.order_status),
        quantity: Number(order.quantity || 1),
    }));
    return {
        orders: normalized,
        total: Array.isArray(res) ? normalized.length : Number(res.total ?? normalized.length),
        truncated: Array.isArray(res) ? false : res.truncated === true,
    };
}

// Cards
const normalizeCard = (item: any): Card => {
  let apiConfig = item.api_config;
  if (typeof apiConfig === 'string' && apiConfig.trim()) {
    try {
      apiConfig = JSON.parse(apiConfig);
    } catch {
      apiConfig = undefined;
    }
  }
  return {...item, api_config: apiConfig || undefined} as Card;
};

const cardPayload = (data: Partial<Card>): Record<string, unknown> => ({
  ...data,
  api_config: data.api_config ? JSON.stringify(data.api_config) : '',
});

export const getCards = async (): Promise<Card[]> => {
  const res = await get<any>('/cards');
  const cards = Array.isArray(res) ? res : (res.cards || []);
  return cards.map(normalizeCard);
};

export const createCard = async (data: Partial<Card>): Promise<{ id: number; message: string }> => {
  return post('/cards', cardPayload(data));
};

export const updateCard = async (cardId: string | number, data: Partial<Card>): Promise<ApiResponse> => {
  return put(`/cards/${cardId}`, cardPayload(data));
};

export const deleteCard = async (cardId: string | number): Promise<ApiResponse> => {
  return del(`/cards/${cardId}`);
};

export const getCardDetails = async (cardId: string | number): Promise<any> => {
  const card = await get<any>(`/cards/${cardId}/details`);
  return normalizeCard(card);
};

// 批量创建卡密组（上传表格）
export const batchCreateCards = async (file: File): Promise<{
  success: boolean;
  total: number;
  created: number;
  failed: number;
  rows: { row_no: number; success: boolean; id?: number; name: string; type?: string; error?: string }[];
}> => {
  const body = new FormData();
  body.append('file', file);
  return postForm('/cards/batch', body);
};

// 往 data 类型卡密组批量追加卡密号
export const appendCardData = async (cardId: string | number, content: string): Promise<{ success: boolean; added: number }> => {
  return post(`/cards/${cardId}/append-data`, { content });
};

// Items
const normalizeBooleanFlag = (value: unknown): boolean =>
    value === true || value === 1 || value === '1';

export const getItems = async (cookieId?: string): Promise<Item[]> => {
    const res = await get<any>('/items', cookieId ? { cookie_id: cookieId } : undefined);
    const items = Array.isArray(res) ? res : (res.items || []);
    return items.map((item: any) => ({
      ...item,
      id: item.id || `${item.cookie_id}-${item.item_id}`,
      is_multi_spec: normalizeBooleanFlag(item.is_multi_spec),
      is_multi_qty_ship: normalizeBooleanFlag(item.is_multi_qty_ship ?? item.multi_quantity_delivery),
      multi_quantity_delivery: normalizeBooleanFlag(item.multi_quantity_delivery ?? item.is_multi_qty_ship),
    }));
}

export const getItem = async (cookieId: string, itemId: string): Promise<Item> =>
  get(`/items/${cookieId}/${itemId}`);

export const saveItemPDDMapping = async (cookieId:string,itemId:string,data:{xianyu_sku_id:string;source_goods_id:string;source_sku_id:string}):Promise<{success:boolean;reconciled_orders:number}> =>
  put(`/items/${encodeURIComponent(cookieId)}/${encodeURIComponent(itemId)}/pdd-mappings`,data);

export const deleteItemPDDMapping = async (cookieId:string,itemId:string,xianyuSkuId:string):Promise<{success:boolean;reconciled_orders:number}> =>
  del(`/items/${encodeURIComponent(cookieId)}/${encodeURIComponent(itemId)}/pdd-mappings/${encodeURIComponent(xianyuSkuId || '_default')}`);


export const syncItemsFromAccount = async (cookieId: string): Promise<any> => {
    return post('/items/get-all-from-account', { cookie_id: cookieId });
}

export const deleteItem = async (cookieId: string, itemId: string): Promise<any> => {
    return del(`/items/${cookieId}/${itemId}`);
}

export const createItem = async (cookieId: string, data: any): Promise<any> => {
    return post(`/items/${cookieId}`, data);
}

export const publishItem = async (form: {
    cookie_id: string;
    title: string;
    description: string;
    price: string;
    original_price?: string;
    quantity: string | number;
    postage_mode: string;
    postage?: string;
    images: File[];
    skus?: Array<{price_cent:number;quantity:number;properties:Array<{name:string;value:string;image_url?:string}>}>;
}): Promise<any> => {
    const body = new FormData();
    body.set('cookie_id', form.cookie_id);
    body.set('title', form.title);
    body.set('description', form.description);
    body.set('price', form.price);
    body.set('original_price', form.original_price || '');
    body.set('quantity', String(form.quantity));
    body.set('postage_mode', form.postage_mode);
    body.set('postage', form.postage || '');
    if (form.skus?.length) body.set('skus', JSON.stringify(form.skus));
    for (const file of form.images) {
      body.append('images', file);
    }
    return postForm('/items/publish', body);
}

export const recommendPublishCategory = async (cookieId: string, keyword: string): Promise<{
    success: boolean;
    category: {
      cat_id: string;
      cat_name: string;
      channel_cat_id: string;
      tb_cat_id?: string;
    };
}> => {
    return post('/items/publish-categories/recommend', { cookie_id: cookieId, keyword });
};

export const previewItemPublishBatch = async (form: {
    file: File;
    imagesZip?: File | null;
    defaultCookieId?: string;
    fallbackCategory: {
      catId: string;
      catName: string;
      channelCatId?: string;
      tbCatId?: string;
    };
}): Promise<any> => {
    const body = new FormData();
    body.set('file', form.file);
    if (form.imagesZip) body.set('images_zip', form.imagesZip);
    if (form.defaultCookieId) body.set('default_cookie_id', form.defaultCookieId);
    body.set('fallback_category_id', form.fallbackCategory.catId);
    body.set('fallback_category_name', form.fallbackCategory.catName);
    body.set('fallback_channel_category_id', form.fallbackCategory.channelCatId || '');
    body.set('fallback_tb_category_id', form.fallbackCategory.tbCatId || '');
    return postForm('/items/publish-batches/preview', body);
}

export const startItemPublishBatch = async (previewId: string): Promise<any> => {
    return post('/items/publish-batches', { preview_id: previewId });
}

export const getItemPublishBatch = async (batchId: string): Promise<any> => {
    return get(`/items/publish-batches/${batchId}`);
}

export const getItemPublishBatches = async (limit = 20): Promise<any[]> => {
    const res = await get<any>('/items/publish-batches', { limit });
    return Array.isArray(res) ? res : (res.batches || []);
}

export const deleteItemPublishBatch = async (batchId: string): Promise<any> => {
    return del(`/items/publish-batches/${batchId}`);
}

export const cancelItemPublishBatch = async (batchId: string): Promise<any> => {
    return post(`/items/publish-batches/${batchId}/cancel`, {});
}

export const retryFailedItemPublishBatch = async (batchId: string): Promise<any> => {
    return post(`/items/publish-batches/${batchId}/retry-failed`, {});
}

export const updateItem = async (cookieId: string, itemId: string, data: Partial<Item>): Promise<any> => {
    return put(`/items/${cookieId}/${itemId}`, data);
}

// Rules - 自动化规则
const normalizeShippingRules = (rules: any[]): ShippingRule[] => rules.map((item: any) => ({
        id: String(item.id),
        name: item.name || '',
        trigger_type: item.trigger_type || 'order_paid',
        item_keyword: item.item_title || item.item_id || '',
        cookie_id: item.cookie_id || '',
        item_id: item.item_id || '',
        item_title: item.item_title || '',
        card_group_id: Number((item.actions || []).find((a: any) => a.action_type === 'send_card')?.card_id || 0),
        card_group_name: (item.actions || []).find((a: any) => a.action_type === 'send_card')?.card_name || '',
        priority: item.priority || 100,
        enabled: item.enabled || false,
        config_json: item.config_json || '{}',
        actions: (item.actions || []).map((action: any) => ({
          id: action.id ? String(action.id) : undefined,
          action_type: action.action_type,
          card_id: Number(action.card_id || 0),
          card_name: action.card_name || '',
          delivery_count: Number(action.delivery_count || 1),
          message_template: action.message_template || '',
          delay_seconds: Number(action.delay_seconds || 0),
          config_json: action.config_json || '{}',
          enabled: action.enabled !== false,
          sort_order: Number(action.sort_order || 0),
        })),
        variants: (item.actions || [])
          .filter((action: any) => action.action_type === 'send_card')
          .map((action: any) => {
            let cfg: any = {};
            try { cfg = JSON.parse(action.config_json || '{}'); } catch {}
            return {
              id: action.id ? String(action.id) : undefined,
              spec_name: cfg.spec_name || '',
              spec_value: cfg.spec_value || '',
              card_id: Number(action.card_id || 0),
              card_name: action.card_name || '',
              delivery_count: Number(action.delivery_count || 1),
              enabled: action.enabled !== false,
              delay_override: cfg.delay_override === true,
              delay_seconds: Number(action.delay_seconds || 0),
              config_json: action.config_json || '{}',
            };
          }),
    }));

export const getShippingRules = async (): Promise<ShippingRule[]> => {
    const res = await get<any>('/automation-rules');
    const rules = Array.isArray(res) ? res : (res.data || res.rules || []);
    return normalizeShippingRules(rules);
}

export interface ShippingRuleListParams {
  cookieId?: string;
  triggerType?: AutomationTriggerType | '';
  enabled?: boolean;
  search?: string;
  page?: number;
  pageSize?: number;
}

export const getShippingRulesPage = async ({
  cookieId,
  triggerType,
  enabled,
  search,
  page = 1,
  pageSize = 10,
}: ShippingRuleListParams = {}): Promise<PaginatedResponse<ShippingRule>> => {
  const res = await get<any>('/automation-rules', {
    page,
    page_size: pageSize,
    cookie_id: cookieId || undefined,
    trigger_type: triggerType || undefined,
    enabled,
    search: search?.trim() || undefined,
  });
  const rules = normalizeShippingRules(Array.isArray(res) ? res : (res.data || res.rules || []));
  return {
    success: true,
    data: rules,
    total: Number(res.total ?? rules.length),
    page: Number(res.page ?? page),
    page_size: Number(res.page_size ?? pageSize),
    total_pages: Number(res.total_pages ?? (rules.length ? 1 : 0)),
    trigger_counts: Object.fromEntries(
      Object.entries(res.trigger_counts || {}).map(([key, value]) => [key, Number(value)]),
    ),
  };
}

const orderAutomationActions = (triggerType: string, actions: AutomationAction[]) => {
    if (triggerType !== 'order_paid') {
      return actions.map((action, index) => ({ ...action, sort_order: action.sort_order || index + 1 }));
    }
    const sendCards = actions
      .filter(action => action.action_type === 'send_card')
      .map((action, index) => ({ ...action, sort_order: index + 1 }));
    const others = actions.filter(action => action.action_type !== 'send_card' && action.action_type !== 'confirm_shipment');
    return [
      ...sendCards,
      ...others.map((action, index) => ({ ...action, sort_order: sendCards.length + index + 1 })),
      { action_type: 'confirm_shipment' as const, enabled: true, sort_order: sendCards.length + others.length + 1 },
    ];
};

export const updateShippingRule = async (rule: Partial<ShippingRule>): Promise<any> => {
    const triggerType = rule.trigger_type || 'order_paid';
    const triggerName: Record<string, string> = {
      order_paid: '付款后自动发货',
      buyer_reviewed: '评价后发送赠品',
      review_missing_timeout: '超时未评价求评价',
    };
    const generatedName = [
      triggerName[triggerType] || '自动化规则',
      rule.item_title || rule.item_id || rule.cookie_id || '',
    ].filter(Boolean).join(' - ');
    const preservedNonCardActions = (rule.actions || []).filter(action => action.action_type !== 'send_card' && action.action_type !== 'confirm_shipment');
    const baseActions: AutomationAction[] = rule.variants && rule.variants.length > 0
      ? [...rule.variants.map((variant, index) => ({
            action_type: 'send_card' as const,
            card_id: variant.card_id,
            delivery_count: variant.delivery_count || 1,
            enabled: variant.enabled !== false,
            sort_order: index + 1,
            delay_seconds: variant.delay_seconds || 0,
            config_json: JSON.stringify({
              spec_name: variant.spec_name || '',
              spec_value: variant.spec_value || '',
              delay_override: variant.delay_override === true,
            }),
		  })), ...preservedNonCardActions]
      : (rule.actions && rule.actions.length > 0 ? rule.actions : [{
          action_type: 'send_card' as const,
          card_id: rule.card_group_id || 0,
          delivery_count: 1,
          enabled: true,
          sort_order: 1,
        }]);
    const actions = orderAutomationActions(triggerType, baseActions);
    const payload = {
        cookie_id: rule.cookie_id || '',
        item_id: rule.item_id || '',
        name: (rule.name || '').trim() || generatedName || '自动化规则',
        trigger_type: triggerType,
        enabled: rule.enabled ?? true,
        priority: rule.priority || 100,
        config_json: rule.config_json || '{}',
        actions: actions.map((action, index) => ({
          action_type: action.action_type,
          card_id: action.card_id || 0,
          delivery_count: action.delivery_count || 1,
          message_template: action.message_template || '',
          delay_seconds: action.delay_seconds || 0,
          config_json: action.config_json || '{}',
          enabled: action.enabled !== false,
          sort_order: action.sort_order || index + 1,
        })),
    };
    return rule.id ? put(`/automation-rules/${rule.id}`, payload) : post('/automation-rules', payload);
}

export const deleteShippingRule = async (id: string): Promise<any> => del(`/automation-rules/${id}`);

export interface AutomationRunIssue {
  id: number;
  cookie_id: string;
  order_id: string;
  trigger_type: string;
  error_message: string;
  issue_kind: 'external_result_unknown' | 'invalid_snapshot' | 'rule_unavailable' | 'partial_failure' | 'execution_failed';
  allowed_resolutions: Array<'continue' | 'retry' | 'cancel'>;
  action_cursor: number;
  sent_count: number;
  updated_at: string;
}

export interface DeferredAutomationIssue {
  id: number;
  cookie_id: string;
  trigger_type: string;
  error_message: string;
  attempt_count: number;
  updated_at: string;
}

export const getAutomationIssues = async (): Promise<{ runs: AutomationRunIssue[]; pending_tasks: DeferredAutomationIssue[] }> => {
  const result = await get<any>('/automation-issues');
  return {
    runs: Array.isArray(result?.runs) ? result.runs : [],
    pending_tasks: Array.isArray(result?.pending_tasks) ? result.pending_tasks : [],
  };
};

export const resolveAutomationRun = async (id: number, resolution: 'continue' | 'retry' | 'cancel'): Promise<any> =>
  post(`/automation-runs/${id}/resolve`, { resolution });

export const resolveDeferredAutomationTask = async (id: number, resolution: 'retry' | 'dismiss'): Promise<any> =>
  post(`/automation-pending-tasks/${id}/resolve`, { resolution });

// Rules - 关键词回复规则 (使用关键词API)
type KeywordRowPayload = {
    id: string;
    keyword: string;
    reply: string;
    item_id: string;
    type: 'text' | 'image';
    image_url: string;
};

const normalizeKeywordRow = (item: any): KeywordRowPayload => ({
    id: String(item?.id || ''),
    keyword: item?.keyword || '',
    reply: item?.reply || '',
    item_id: item?.item_id || '',
    type: item?.type === 'image' ? 'image' : 'text',
    image_url: item?.image_url || '',
});

const getKeywordRowsWithType = async (cookieId: string): Promise<KeywordRowPayload[]> => {
    const existing = await get<any>(`/keywords-with-type/${cookieId}`);
    return Array.isArray(existing) ? existing.map(normalizeKeywordRow) : [];
};

export const getReplyRules = async (cookieId?: string): Promise<ReplyRule[]> => {
    if (!cookieId) return [];
    const keywords = await getKeywordRowsWithType(cookieId);
	return keywords.map((item: any) => ({
		id: item.id,
        keyword: item.keyword || '',
        reply_content: item.reply || '',
        match_type: 'fuzzy' as const,
        enabled: true,
        item_id: item.item_id || '',
        type: item.type === 'image' ? 'image' : 'text',
        image_url: item.image_url || ''
    }));
}

export const updateReplyRule = async (rule: Partial<ReplyRule>, cookieId: string): Promise<any> => {
	const type = rule.type || 'text';
	const payload = {
		keyword: rule.keyword || '',
		reply: type === 'text' ? (rule.reply_content || '') : '',
		item_id: rule.item_id || '',
		type,
		image_url: type === 'image' ? (rule.image_url || '') : '',
	};
	return rule.id
		? put(`/keywords-with-type/${cookieId}/${rule.id}`, payload)
		: post(`/keywords-with-item-id/${cookieId}`, payload);
}

export const deleteReplyRule = async (id: string, cookieId: string): Promise<any> => {
	return del(`/keywords-with-type/${cookieId}/${id}`);
}

// Settings
export const getSystemSettings = async (): Promise<SystemSettings> => {
    const res = await get<{data: SystemSettings}>('/system-settings');
    return normalizeSettings(res.data || res); // handle {success:true, data: {...}} wrapper if exists
};

export const updateSystemSettings = async (settings: Partial<SystemSettings>): Promise<ApiResponse> => {
	const payload = Object.fromEntries(
		Object.entries(settings).filter(([, value]) => value !== undefined && value !== null),
	);
	return put('/system-settings', payload);
};

export const getPDDAccount = async (): Promise<PDDAccountConfig> => get('/api/pdd/account');
export const savePDDAccount = async (input: {name:string;site:'pinduoduo'|'yangkeduo';cookie?:string;default_address_id:string;user_agent?:string;enabled:boolean}): Promise<PDDAccountConfig> => put('/api/pdd/account',input);
export const verifyPDDAccount = async (): Promise<{success:boolean;credential_status:string;pdd_uid:string;message:string}> => post('/api/pdd/account/verify',{});
export const deletePDDAccount = async (): Promise<ApiResponse> => del('/api/pdd/account');

export interface FulfillmentAPIKey {
	id: string;
	name: string;
	enabled: boolean;
	last_used_at: number;
	created_at: number;
}

export interface CreatedFulfillmentAPIKey {
	id: string;
	name: string;
	api_key: string;
	created_at: number;
}

export const getFulfillmentAPIKeys = async (): Promise<FulfillmentAPIKey[]> =>
	get('/api/fulfillment/keys');

export const createFulfillmentAPIKey = async (name: string): Promise<CreatedFulfillmentAPIKey> =>
	post('/api/fulfillment/keys', { name });

export const revokeFulfillmentAPIKey = async (id: string): Promise<ApiResponse> =>
	del(`/api/fulfillment/keys/${encodeURIComponent(id)}`);

export const getAccountAISettings = async (cookieId: string, options?: RequestControlOptions): Promise<AIReplySettings> => {
    return get(`/ai-reply-settings/${cookieId}`, undefined, options);
}

export const updateAccountAISettings = async (cookieId: string, settings: Partial<AIReplySettings>): Promise<ApiResponse> => {
  const payload = {
    ai_enabled: settings.ai_enabled ?? false,
    max_discount_percent: settings.max_discount_percent ?? 10,
    max_discount_amount: settings.max_discount_amount ?? 100,
    max_bargain_rounds: settings.max_bargain_rounds ?? 3,
    custom_prompts: settings.custom_prompts ?? ''
  };
  return put(`/ai-reply-settings/${cookieId}`, payload);
}

export const fetchAIModels = async (baseUrl: string, apiKey: string = ''): Promise<string[]> => {
  const result = await post<{ models?: string[] }>('/ai-models', {
    base_url: baseUrl,
    api_key: apiKey,
  });
  return result.models || [];
};

// Notification Channels
const parseNotificationEventTypes = (raw: unknown): NotificationEventType[] => {
  if (Array.isArray(raw)) return raw.filter(Boolean) as NotificationEventType[];
  if (typeof raw !== 'string' || !raw.trim()) return [];
  try {
    const parsed = JSON.parse(raw);
    if (Array.isArray(parsed)) return parsed.filter(Boolean) as NotificationEventType[];
  } catch {
    // fall back to legacy comma/semicolon separated values
  }
  return raw.split(/[,\s;]+/).map(v => v.trim()).filter(Boolean) as NotificationEventType[];
};

const stringifyNotificationEventTypes = (events?: NotificationEventType[]): string => {
  const clean = Array.from(new Set((events || []).filter(Boolean)));
  return clean.length > 0 ? JSON.stringify(clean) : '';
};

export const getNotificationChannels = async (options?: RequestControlOptions): Promise<{ success: boolean; data?: NotificationChannel[] }> => {
  const result = await get<any[]>('/notification-channels', undefined, options);
  const channels = (result || []).map((item: any) => {
    let parsedConfig;
    try {
      parsedConfig = typeof item.config === 'string' ? JSON.parse(item.config) : item.config;
    } catch {
		parsedConfig = {};
    }
    return {
      id: String(item.id),
      name: item.name,
		type: item.type === 'ding_talk' ? 'dingtalk' : (item.type === 'lark' ? 'feishu' : item.type),
      config: parsedConfig,
      event_types: parseNotificationEventTypes(item.event_types),
      enabled: item.enabled,
      created_at: item.created_at,
      updated_at: item.updated_at,
    };
  });
  return { success: true, data: channels };
}

export const createNotificationChannel = async (data: { name: string; type: string; config: Record<string, unknown>; event_types?: NotificationEventType[]; enabled?: boolean }): Promise<ApiResponse> => {
  return post('/notification-channels', {
    ...data,
    config: JSON.stringify(data.config),
    event_types: stringifyNotificationEventTypes(data.event_types)
  });
}

export const updateNotificationChannel = async (channelId: string, data: { name?: string; type?: string; config?: Record<string, unknown>; event_types?: NotificationEventType[]; enabled?: boolean }): Promise<ApiResponse> => {
  const payload: Record<string, unknown> = { ...data };
  if ('config' in data) {
    payload.config = JSON.stringify(data.config);
  }
  if ('event_types' in data) {
    payload.event_types = stringifyNotificationEventTypes(data.event_types);
  }
  return put(`/notification-channels/${channelId}`, payload);
}

export const deleteNotificationChannel = async (channelId: string): Promise<ApiResponse> => {
  return del(`/notification-channels/${channelId}`);
}

// Message Notifications
export const getMessageNotifications = async (): Promise<{ success: boolean; data?: any[] }> => {
  const result = await get<Record<string, any[]>>('/message-notifications');
  const notifications = [];
  for (const [cookieId, channelList] of Object.entries(result || {})) {
    if (Array.isArray(channelList)) {
      for (const item of channelList) {
        notifications.push({
          cookie_id: cookieId,
          channel_id: item.channel_id,
          channel_name: item.channel_name,
          enabled: item.enabled,
        });
      }
    }
  }
  return { success: true, data: notifications };
}

export const setMessageNotification = async (cookieId: string, channelId: number, enabled: boolean): Promise<ApiResponse> => {
  return post(`/message-notifications/${cookieId}`, { channel_id: channelId, enabled });
}

export const deleteMessageNotification = async (notificationId: string): Promise<ApiResponse> => {
  return del(`/message-notifications/${notificationId}`);
}

export const deleteAccountNotifications = async (cookieId: string): Promise<ApiResponse> => {
  return del(`/message-notifications/account/${cookieId}`);
}

// 账号 ↔ 渠道 绑定（覆盖式）
export const getAccountBindings = async (cookieId: string, options?: RequestControlOptions): Promise<number[]> => {
  const result = await get<{ cookie_id: string; channel_ids: number[] }>(`/message-notifications/${cookieId}`, undefined, options);
  return result?.channel_ids || [];
}

export const setAccountBindings = async (cookieId: string, channelIds: number[]): Promise<ApiResponse> => {
  return post(`/message-notifications/${cookieId}`, { channel_ids: channelIds });
}

// 测试发送
export const testNotificationChannel = async (channelId: string): Promise<ApiResponse> => {
  return post(`/notification-channels/${channelId}/test`, {});
}

// Default Reply
export const getDefaultReplies = async (): Promise<Record<string, DefaultReply>> => {
  return get('/api/default-replies');
};

export const getDefaultReply = async (cookieId: string): Promise<DefaultReply> => {
  const result = await get<any>(`/api/default-reply/${cookieId}`);
  return {
    cookie_id: cookieId,
    enabled: result.enabled || false,
    reply_content: result.reply_content || '',
    reply_once: result.reply_once || false,
    reply_image_url: result.reply_image_url || ''
  };
};

export const updateDefaultReply = async (cookieId: string, data: Partial<DefaultReply>): Promise<ApiResponse> => {
  return put(`/api/default-reply/${cookieId}`, {
    enabled: data.enabled ?? false,
    reply_content: data.reply_content || '',
    reply_once: data.reply_once ?? false,
    reply_image_url: data.reply_image_url || ''
  });
};

export const deleteDefaultReply = async (cookieId: string): Promise<ApiResponse> => {
  return del(`/api/default-reply/${cookieId}`);
};

export const clearDefaultReplyRecords = async (cookieId: string): Promise<ApiResponse> => {
  return post(`/api/default-reply/${cookieId}/clear-records`, {});
};

export interface PDDCollectorDevice {
  id: string;
  name: string;
  enabled: boolean;
  last_seen_at: number;
  last_collected_at: number;
  created_at: number;
}
export interface CreatedPDDCollectorDevice extends PDDCollectorDevice { device_token: string }
export const getPDDCollectorDevices = (): Promise<PDDCollectorDevice[]> => get('/api/pdd-collector/devices');
export const createPDDCollectorDevice = (name: string): Promise<CreatedPDDCollectorDevice> => post('/api/pdd-collector/devices', { name });

export interface PDDSpec { spec_key: string; spec_key_id?: string; spec_value_id?: string; raw_value: string }
export interface PDDGoodsProperty { key:string; values:string[]; ref_pid?:string; reference_id?:string }
export interface PDDSKU {
  id: number; sku_id: string; specs: PDDSpec[]; spec_value_ids: string[]; thumb_url: string;
  prices: Record<string, unknown>; price_cent: number; stock: number; stock_exact: boolean; is_onsale: boolean; last_collected_at: number;
}
export interface PDDProductSummary {
  id: number; goods_id: string; mall_sn:string; final_url: string; title: string; images: string[];
  first_collected_at: number; last_collected_at: number; sku_count: number; onsale_sku_count: number;
  min_price_cent: number; max_price_cent: number;
}
export interface PDDProductDetail extends Omit<PDDProductSummary, 'sku_count' | 'onsale_sku_count' | 'min_price_cent' | 'max_price_cent'> { goods_property:PDDGoodsProperty[]; skus: PDDSKU[] }
export const getPDDProducts = (): Promise<PDDProductSummary[]> => get('/api/pdd-collector/catalog');
export const getPDDProduct = (goodsId: string): Promise<PDDProductDetail> => get(`/api/pdd-collector/catalog/${encodeURIComponent(goodsId)}`);
export interface PDDProductRefreshResult {
  success: boolean; goods_id: string; sku_count: number; added: number; price_changed: number;
  stock_changed: number; status_changed: number; missing_suspected: string[];
  material_stock_updates: number; updated_at: number;
}
export const refreshPDDProduct = (goodsId:string):Promise<PDDProductRefreshResult> => post(`/api/pdd-collector/catalog/${encodeURIComponent(goodsId)}/refresh`,{});
export const deletePDDProduct = (goodsId:string):Promise<{draft_count:number;message:string}> => del(`/api/pdd-collector/catalog/${encodeURIComponent(goodsId)}`);
export interface ProductMaterialSKU { material_sku_id?:string; source_goods_id?:string; source_sku_id?:string; source_properties?:Array<{name:string;value:string}>; source_image_url?:string; price_cent:number; quantity:number; enabled:boolean; image_url?:string; properties:Array<{name:string;value:string;image_url?:string}> }
export interface ProductMaterialVideo { source:'review'|'product'|'upload';source_goods_id?:string;review_id?:string;sku_id?:string;url:string;cover_url?:string;duration_ms?:number }
export interface ProductMaterial { id:number; source_type:string; source_id:string; source_ids?:string[]; title:string; description:string; images:string[]; category:Record<string,unknown>; skus:ProductMaterialSKU[]; postage_mode:string; postage_cent:number; image_property_name:string; video_enabled:boolean; videos:ProductMaterialVideo[]; status:string; updated_at:number }
export interface PDDReviewMedia { id:number;goods_id:string;review_id:string;sku_id:string;media_type:'image'|'video';source_type:'initial'|'additional';url:string;cover_url:string;width:number;height:number;duration_ms:number;is_live_photo_image:boolean }
export interface MaterialPublishRecord { id:number; cookie_id:string; published_item_id:string; status:string; error_message:string; created_at:number; finished_at:number; mapping_counts?:Record<'pending'|'mapped'|'unmapped'|'ambiguous',number> }
export const createMaterialFromPDD = (goodsId:string):Promise<{id:number}> => post(`/materials/from-pdd/${encodeURIComponent(goodsId)}`,{});
export const getMaterials = (query = ''):Promise<ProductMaterial[]> => get(`/materials${query.trim() ? `?q=${encodeURIComponent(query.trim())}` : ''}`);
export const updateMaterial = (id:number,data:Omit<ProductMaterial,'id'|'source_type'|'source_id'|'status'|'updated_at'>) => put(`/materials/${id}`,data);
export const deleteMaterial = (id:number) => del(`/materials/${id}`);
export const uploadMaterialImage = (file:File):Promise<{url:string}> => { const body=new FormData();body.append('image',file);return postForm('/materials/images',body) };
export const getPDDReviewMedia = (goodsId:string,type:'image'|'video'):Promise<PDDReviewMedia[]> => get(`/api/pdd-collector/catalog/${encodeURIComponent(goodsId)}/review-media?type=${type}`);
export const publishMaterial = (id:number,cookieId:string):Promise<any> => post(`/materials/${id}/publish`,{cookie_id:cookieId},{timeoutMs:120_000});
export const getMaterialPublishRecords = (id:number):Promise<MaterialPublishRecord[]> => get(`/materials/${id}/publish-records`);
export const getMaterialSourceDiff = (id:number):Promise<{added:any[];changed:any[];removed:string[]}> => get(`/materials/${id}/source-diff`);
export const syncMaterialSource = (id:number,options:{prices:boolean;stock:boolean;images:boolean;add_new:boolean;disable_removed:boolean}) => post(`/materials/${id}/sync-source`,options);

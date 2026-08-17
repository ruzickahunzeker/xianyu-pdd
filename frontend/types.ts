
// API Response Bases
export interface ApiResponse {
  success?: boolean;
  message?: string;
  msg?: string;
}

export interface PaginatedResponse<T> {
  success: boolean;
  data: T[];
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
  trigger_counts?: Record<string, number>;
}

// Auth
export interface LoginResponse {
  success: boolean;
  token?: string;
  message?: string;
  user_id?: number;
  username?: string;
  is_admin?: boolean;
}

// Accounts
export interface AccountDetail {
  id: string;
  value?: string; // cookie value from backend
  cookie?: string; // alias for value
  enabled: boolean;
  auto_confirm: boolean;
  remark?: string;
  note?: string; // alias for remark
  pause_duration?: number;
  paused_until?: number;
  paused?: boolean;
  // 登录信息
  username?: string;
  login_password?: string;
  show_browser?: boolean;
  // Frontend helpers
  nickname?: string;
  avatar_url?: string;
  profile_error?: string;
  runtime_state?: 'starting' | 'connecting' | 'online' | 'reconnecting' | 'auth_expired' | 'verification_required' | 'error' | 'stopped' | 'disabled';
  runtime_message?: string;
  runtime_connected?: boolean;
  runtime_updated_at?: string;
  // AI设置
  ai_enabled?: boolean;
  max_discount_percent?: number;
  max_discount_amount?: number;
  max_bargain_rounds?: number;
  custom_prompts?: string;
	// 账号级计划任务
	auto_rate_enabled?: boolean;
	rate_content?: string;
	auto_polish_enabled?: boolean;
	polish_time?: string;
	last_rate_scan_at?: number;
	last_polish_date?: string;
	last_polish_at?: number;
}

export interface AccountTaskSettings {
	account_id: string;
	auto_rate_enabled: boolean;
	rate_content: string;
	auto_polish_enabled: boolean;
	polish_time: string;
	last_rate_scan_at?: number;
	last_polish_date?: string;
	last_polish_at?: number;
}

export interface AccountTaskSummary {
	task_type: 'auto_rate' | 'auto_polish';
	found: number;
	success: number;
	failed: number;
	skipped: number;
	message?: string;
}

export interface ChatSession {
	account_id: string;
	chat_id: string;
	buyer_id: string;
	buyer_name: string;
	buyer_avatar_url?: string;
	item_id?: string;
	item_title?: string;
	last_message: string;
	last_message_at: number;
	unread_count: number;
}

export interface ChatMessage {
	id: number;
	account_id: string;
	chat_id: string;
	message_key: string;
	direction: 'incoming' | 'outgoing';
	sender_id: string;
	sender_name: string;
	/** text/image/video are peer messages; system is an official platform notice or trade card. */
	message_type: 'text' | 'image' | 'video' | 'system';
	content: string;
	status: 'received' | 'sending' | 'sent' | 'failed';
	sent_at: number;
}

// Orders
export type OrderStatus = 
  | 'processing'      
  | 'pending_ship'    
  | 'shipped'         
  | 'completed'       
  | 'cancelled'       
  | 'refunding'
  | 'unknown';

export interface Order {
  id: string;
  order_id: string;
  cookie_id: string;
  item_id: string;
  item_title?: string;
  item_image?: string;
  item_price?: string;
  buyer_id: string;
  quantity: number;
  amount: string;
  status: OrderStatus;
  order_status?: OrderStatus;
  receiver_name?: string;
  receiver_phone?: string;
  receiver_address?: string;
  created_at?: string;
  updated_at?: string;
}

// Cards
export interface Card {
  id: number;
  name: string;
  type: 'api' | 'text' | 'data' | 'image';
  description?: string;
  enabled: boolean;
  // 文本类型
  text_content?: string;
  // 批量数据类型
  data_content?: string;
  // API 类型配置
  api_config?: {
    url: string;
    method: 'GET' | 'POST';
    timeout?: number;
    headers?: string;
    params?: string;
  };
  // 图片类型
  image_url?: string;
  // 通用配置
  delay_seconds?: number;
  // 多规格配置
  is_multi_spec?: boolean;
  spec_name?: string;
  spec_value?: string;
  created_at: string;
  updated_at: string;
}

// Items
export interface Item {
  id: string | number;
  cookie_id: string;
  item_id: string;
  item_title?: string;
  item_description?: string;
  item_price?: string;
  item_image?: string; // Inferred from common usage, though not explicitly in list model sometimes
  item_category?: string;
  item_detail?: string;
  is_multi_spec?: number | boolean;
  multi_quantity_delivery?: number | boolean;
  is_multi_qty_ship?: number | boolean;
  created_at?: string;
  remote_detail?: ItemRemoteDetail;
  pdd_mapping?: ItemPDDMappingDetail;
}

export interface ItemPDDMappingRow { xianyu_sku_id:string; source_goods_id:string; source_sku_id:string; mapping_source:string; pdd_title:string; pdd_specs:Array<{spec_key?:string;raw_value?:string}>; pdd_thumb_url?:string; pdd_price_cent:number; pdd_stock:number; pdd_onsale:boolean; source_exists:boolean; }
export interface ItemPDDMappingDetail { status:'mapped'|'partial'|'unmapped'; mapped:number; total:number; rows?:ItemPDDMappingRow[]; }

export interface ItemRemoteProperty { propertyId?: number|string; propertyText?: string; valueId?: number|string; valueText?: string; actualValueText?: string; }
export interface ItemRemoteSKU { sku_id:string; inventory_id:string; price_cent:number; quantity:number; properties:ItemRemoteProperty[]; property_image_url?:string; enabled:boolean; status:number; sort_order:number; }
export interface ItemRemoteDetail { description:string; images:string[]; category:Record<string,unknown>; min_price_cent:number; max_price_cent:number; total_quantity:number; item_status:number; item_status_text:string; transport_fee:string; synced_at:number; sku_count:number; skus:ItemRemoteSKU[]; }

export type AutomationTriggerType = 'order_paid' | 'buyer_reviewed' | 'review_missing_timeout';
export type AutomationActionType = 'confirm_shipment' | 'send_card' | 'send_text';

// Rules
export interface ShippingRule {
  id: string;
  name: string;
  trigger_type: AutomationTriggerType;
  item_keyword: string; // Legacy UI helper
  cookie_id?: string;
  item_id?: string;
  item_title?: string;
  card_group_id: number; // First send_card action card id
  card_group_name?: string; // UI helper
  priority: number;
  enabled: boolean;
  config_json?: string;
  actions: AutomationAction[];
  variants: ShippingVariant[];
}

export interface AutomationAction {
  id?: string;
  action_type: AutomationActionType;
  card_id?: number;
  card_name?: string;
  delivery_count?: number;
  message_template?: string;
  delay_seconds?: number;
  config_json?: string;
  enabled: boolean;
  sort_order?: number;
}

export interface ShippingVariant {
  id?: string;
  spec_name: string;
  spec_value: string;
  card_id: number;
  card_name?: string;
  card_type?: Card['type'];
  delivery_count: number;
  enabled: boolean;
  delay_override?: boolean;
  delay_seconds?: number;
  config_json?: string;
}

export interface ReplyRule {
  id: string;
  keyword: string;
  reply_content: string;
  match_type: 'exact' | 'fuzzy';
  enabled: boolean;
  item_id?: string;
  type?: 'text' | 'image';
  image_url?: string;
}

// Stats
export interface AdminStats {
  total_users: number;
  total_cookies: number;
  active_cookies: number;
  total_cards: number;
  total_keywords: number;
  total_orders: number;
}

export interface DashboardStats {
  total_cookies: number;
  active_cookies: number;
  total_cards: number;
  total_keywords: number;
  total_orders: number;
  available_card_stock: number;
}

export interface OrderAnalytics {
  revenue_stats: {
    total_amount: number;
    total_orders: number;
  };
  daily_stats: Array<{ date: string; amount: number; order_count: number }>;
  item_stats?: Array<{
    item_id: string;
    order_count: number;
    total_amount: number;
    avg_amount: number;
  }>;
}

// Settings
export interface SystemSettings {
  ai_model?: string;
  ai_api_key?: string;
  ai_api_url?: string;
  ai_base_url?: string;
  default_reply?: string;
  registration_enabled?: boolean;
  smtp_server?: string;
  log_level?: 'debug' | 'info' | 'warn' | 'error' | string;
  log_format?: 'text' | 'json' | string;
  renewal_log_retention_days?: number;
  order_sync_enabled?: boolean | string;
  order_sync_interval_minutes?: number;
  pdd_product_refresh_interval_hours?: number;
  'captcha.remote_service_url'?: string;
  'captcha.remote_secret_key'?: string;
  'captcha.remote_pass_cookies'?: boolean | string;
  [key: string]: any;
}

export interface PDDAccountConfig {
  id: string;
  name: string;
  pdd_uid: string;
  default_address_id: string;
  user_agent: string;
  enabled: boolean;
  configured: boolean;
  cookie_configured: boolean;
  credential_status: 'unconfigured' | 'unchecked' | 'valid' | 'invalid' | 'expired' | 'unknown';
  last_verified_at: number;
  last_error: string;
}

export interface AIReplySettings {
  ai_enabled: boolean;
  model_name?: string;
  api_key?: string;
  base_url?: string;
  max_discount_percent: number;
  max_discount_amount?: number;
  max_bargain_rounds: number;
  custom_prompts: string;
}

// Default Reply
export interface DefaultReply {
  cookie_id: string;
  enabled: boolean;
  reply_content: string;
  reply_once: boolean;
  reply_image_url?: string;
}

// 通知渠道
export type NotificationChannelType = 'dingtalk' | 'feishu' | 'bark' | 'webhook' | 'wechat' | 'telegram' | 'email';
export type NotificationEventType =
  | 'account_offline'
  | 'account_recovered'
  | 'account_disabled'
  | 'security_verification'
  | 'token_renewal'
  | 'delivery_result'
  | 'system_error';

export interface NotificationChannel {
  id: string;
  name: string;
  type: NotificationChannelType;
  config: Record<string, unknown>;
  event_types?: NotificationEventType[];
  enabled: boolean;
  created_at?: string;
  updated_at?: string;
}

export interface TelegramUser {
  id: number;
  first_name: string;
  last_name?: string;
  username?: string;
  photo_url?: string;
  auth_date: number;
  hash: string;
}

export interface AuthState {
  user: TelegramUser | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  error: string | null;
}

export interface AuthContextType extends AuthState {
  login: (user: TelegramUser) => Promise<void>;
  logout: () => Promise<void>;
  checkAuth: () => Promise<void>;
}

// Telegram account (added by user for sending messages)
export interface TelegramAccount {
  id: string;
  telegram_id: number;
  phone: string;
  first_name: string;
  last_name?: string;
  username?: string;
  photo_url?: string;
  is_active: boolean;
  created_at: string;
  has_openai_token?: boolean;
}

// QR code auth state
export interface QRAuthState {
  status: 'idle' | 'pending' | 'scanning' | 'password_required' | 'success' | 'error';
  qr_url?: string;
  token?: string;
  error?: string;
}

// Spam status from @SpamBot
export interface SpamStatus {
  is_limited: boolean;
  limited_until?: string;
  message: string;
  checked_at: string;
  from_cache: boolean;
}

// Contact (verified phone number)
export interface Contact {
  id: string;
  account_id: string;
  telegram_id: string;  // Serialized as string to preserve int64 precision
  access_hash: string;  // Serialized as string to preserve int64 precision
  phone: string;
  first_name: string;
  last_name?: string;
  username?: string;
  photo_url?: string;
  labels?: string[];
  is_valid: boolean;
  created_at: string;
  updated_at: string;
}

// Check numbers result
export interface CheckNumbersResult {
  valid: Contact[];
  invalid: string[];
  retry: string[];
  errors: string[];
  total: number;
  valid_count: number;
}

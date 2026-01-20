import { createContext, useContext, useState, useCallback, useEffect, useRef, ReactNode } from 'react';
import { TelegramAccount, QRAuthState } from '../types';

const API_URL = import.meta.env.VITE_API_URL || '';

interface AccountsContextType {
  accounts: TelegramAccount[];
  selectedAccount: TelegramAccount | null;
  isLoading: boolean;
  error: string | null;
  qrAuth: QRAuthState;
  fetchAccounts: () => Promise<void>;
  selectAccount: (account: TelegramAccount | null) => void;
  startQRAuth: () => Promise<void>;
  cancelQRAuth: () => void;
  submitPassword: (password: string) => Promise<void>;
  removeAccount: (id: string) => Promise<void>;
}

const AccountsContext = createContext<AccountsContextType | undefined>(undefined);

export function AccountsProvider({ children }: { children: ReactNode }) {
  const [accounts, setAccounts] = useState<TelegramAccount[]>([]);
  const [selectedAccount, setSelectedAccount] = useState<TelegramAccount | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [qrAuth, setQRAuth] = useState<QRAuthState>({ status: 'idle' });
  const pollIntervalRef = useRef<number | null>(null);

  const fetchAccounts = useCallback(async () => {
    try {
      setIsLoading(true);
      setError(null);

      const response = await fetch(`${API_URL}/api/accounts`, {
        credentials: 'include',
      });

      if (!response.ok) {
        throw new Error('Failed to fetch accounts');
      }

      const data = await response.json();
      setAccounts(data.accounts || []);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch accounts');
    } finally {
      setIsLoading(false);
    }
  }, []);

  const selectAccount = useCallback(async (account: TelegramAccount | null) => {
    setSelectedAccount(account);

    if (account) {
      // Validate session when selecting an account
      try {
        const response = await fetch(`${API_URL}/api/accounts/${account.id}/validate`, {
          credentials: 'include',
        });

        if (response.ok) {
          const data = await response.json();
          // Update account in the list with new status and photo
          if (data.account) {
            const updates = {
              is_active: data.is_active,
              photo_url: data.account.photo_url || account.photo_url
            };
            setAccounts(prev => prev.map(a =>
              a.id === account.id ? { ...a, ...updates } : a
            ));
            setSelectedAccount(prev => prev ? { ...prev, ...updates } : null);
          }
        }
      } catch (err) {
        console.error('Failed to validate account:', err);
      }
    }
  }, []);

  const startQRAuth = useCallback(async () => {
    // Cancel any existing polling first
    if (pollIntervalRef.current) {
      window.clearInterval(pollIntervalRef.current);
      pollIntervalRef.current = null;
    }

    try {
      setQRAuth({ status: 'pending' });

      const response = await fetch(`${API_URL}/api/accounts/qr/start`, {
        method: 'POST',
        credentials: 'include',
      });

      if (!response.ok) {
        throw new Error('Failed to start QR authentication');
      }

      const data = await response.json();

      // Use status from server response
      setQRAuth({
        status: data.status || 'scanning',
        qr_url: data.qr_url,
        token: data.token,
      });

      // If already success (unlikely but possible)
      if (data.status === 'success') {
        fetchAccounts();
        return;
      }

      // If error
      if (data.status === 'error') {
        setQRAuth({ status: 'error', error: data.error || 'Failed to start QR authentication' });
        return;
      }

      // Start polling for QR scan result
      const interval = window.setInterval(async () => {
        // Check if this interval is still the active one
        if (pollIntervalRef.current !== interval) {
          window.clearInterval(interval);
          return;
        }
        try {
          const pollResponse = await fetch(`${API_URL}/api/accounts/qr/status?token=${data.token}`, {
            credentials: 'include',
          });

          if (!pollResponse.ok) {
            // Session not found - might be expired or completed
            if (pollResponse.status === 404) {
              setQRAuth({ status: 'error', error: 'Session expired. Please try again.' });
              window.clearInterval(interval);
              pollIntervalRef.current = null;
            }
            return;
          }

          const pollData = await pollResponse.json();

          if (pollData.status === 'success') {
            setQRAuth({ status: 'success' });
            window.clearInterval(interval);
            pollIntervalRef.current = null;
            fetchAccounts();
          } else if (pollData.status === 'error') {
            setQRAuth({ status: 'error', error: pollData.error });
            window.clearInterval(interval);
            pollIntervalRef.current = null;
          } else if (pollData.status === 'expired') {
            setQRAuth({ status: 'error', error: 'QR code expired. Please try again.' });
            window.clearInterval(interval);
            pollIntervalRef.current = null;
          } else if (pollData.status === 'password_required') {
            // 2FA required - update state but keep polling
            setQRAuth(prev => ({
              ...prev,
              status: 'password_required',
            }));
          } else if (pollData.status === 'scanning' && pollData.qr_url) {
            // Update QR code if it changed (token refresh)
            setQRAuth(prev => ({
              ...prev,
              status: pollData.status,
              qr_url: pollData.qr_url,
            }));
          }
        } catch (err) {
          console.error('Poll error:', err);
          // Continue polling on network errors
        }
      }, 2000);

      pollIntervalRef.current = interval;
    } catch (err) {
      setQRAuth({
        status: 'error',
        error: err instanceof Error ? err.message : 'Failed to start QR authentication',
      });
    }
  }, [fetchAccounts]);

  const cancelQRAuth = useCallback(() => {
    if (pollIntervalRef.current) {
      window.clearInterval(pollIntervalRef.current);
      pollIntervalRef.current = null;
    }
    setQRAuth({ status: 'idle' });
  }, []);

  const submitPassword = useCallback(async (password: string) => {
    try {
      if (!qrAuth.token) {
        throw new Error('No active QR session');
      }

      setQRAuth(prev => ({ ...prev, status: 'pending' }));

      const response = await fetch(`${API_URL}/api/accounts/qr/password`, {
        method: 'POST',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          token: qrAuth.token,
          password: password,
        }),
      });

      const data = await response.json();

      if (!response.ok) {
        // Restore password_required state on error so user can retry
        setQRAuth(prev => ({
          ...prev,
          status: 'password_required',
          error: data.error || 'Failed to verify password',
        }));
        return;
      }

      if (data.status === 'success') {
        setQRAuth({ status: 'success' });
        if (pollIntervalRef.current) {
          window.clearInterval(pollIntervalRef.current);
          pollIntervalRef.current = null;
        }
        fetchAccounts();
      } else if (data.status === 'error') {
        setQRAuth(prev => ({
          ...prev,
          status: 'password_required',
          error: data.error || 'Invalid password',
        }));
      }
    } catch (err) {
      setQRAuth(prev => ({
        ...prev,
        status: 'password_required',
        error: err instanceof Error ? err.message : 'Failed to submit password',
      }));
    }
  }, [qrAuth.token, fetchAccounts]);

  const removeAccount = useCallback(async (id: string) => {
    try {
      const response = await fetch(`${API_URL}/api/accounts/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      });

      if (!response.ok) {
        throw new Error('Failed to remove account');
      }

      setAccounts(prev => prev.filter(a => a.id !== id));
      if (selectedAccount?.id === id) {
        setSelectedAccount(null);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to remove account');
    }
  }, [selectedAccount]);

  // Cleanup polling on unmount
  useEffect(() => {
    return () => {
      if (pollIntervalRef.current) {
        window.clearInterval(pollIntervalRef.current);
      }
    };
  }, []);

  // Fetch accounts on mount
  useEffect(() => {
    fetchAccounts();
  }, [fetchAccounts]);

  return (
    <AccountsContext.Provider
      value={{
        accounts,
        selectedAccount,
        isLoading,
        error,
        qrAuth,
        fetchAccounts,
        selectAccount,
        startQRAuth,
        cancelQRAuth,
        submitPassword,
        removeAccount,
      }}
    >
      {children}
    </AccountsContext.Provider>
  );
}

export function useAccounts(): AccountsContextType {
  const context = useContext(AccountsContext);
  if (context === undefined) {
    throw new Error('useAccounts must be used within an AccountsProvider');
  }
  return context;
}

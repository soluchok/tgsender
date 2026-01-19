import { createContext, useContext, useState, useCallback, useEffect, ReactNode } from 'react';
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
  const [pollInterval, setPollInterval] = useState<number | null>(null);

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

  const selectAccount = useCallback((account: TelegramAccount | null) => {
    setSelectedAccount(account);
  }, []);

  const startQRAuth = useCallback(async () => {
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
        try {
          const pollResponse = await fetch(`${API_URL}/api/accounts/qr/status?token=${data.token}`, {
            credentials: 'include',
          });

          if (!pollResponse.ok) {
            // Session not found - might be expired or completed
            if (pollResponse.status === 404) {
              setQRAuth({ status: 'error', error: 'Session expired. Please try again.' });
              window.clearInterval(interval);
              setPollInterval(null);
            }
            return;
          }

          const pollData = await pollResponse.json();
          console.log('Poll response:', pollData);

          if (pollData.status === 'success') {
            setQRAuth({ status: 'success' });
            window.clearInterval(interval);
            setPollInterval(null);
            fetchAccounts();
          } else if (pollData.status === 'error') {
            setQRAuth({ status: 'error', error: pollData.error });
            window.clearInterval(interval);
            setPollInterval(null);
          } else if (pollData.status === 'expired') {
            setQRAuth({ status: 'error', error: 'QR code expired. Please try again.' });
            window.clearInterval(interval);
            setPollInterval(null);
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
              qr_url: pollData.qr_url,
            }));
          }
        } catch (err) {
          console.error('Poll error:', err);
          // Continue polling on network errors
        }
      }, 2000);

      setPollInterval(interval);
    } catch (err) {
      setQRAuth({
        status: 'error',
        error: err instanceof Error ? err.message : 'Failed to start QR authentication',
      });
    }
  }, [fetchAccounts]);

  const cancelQRAuth = useCallback(() => {
    if (pollInterval) {
      window.clearInterval(pollInterval);
      setPollInterval(null);
    }
    setQRAuth({ status: 'idle' });
  }, [pollInterval]);

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
        if (pollInterval) {
          window.clearInterval(pollInterval);
          setPollInterval(null);
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
  }, [qrAuth.token, pollInterval, fetchAccounts]);

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
      if (pollInterval) {
        window.clearInterval(pollInterval);
      }
    };
  }, [pollInterval]);

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

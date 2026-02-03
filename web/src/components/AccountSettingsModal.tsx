import { useState, useEffect } from 'react';
import { TelegramAccount } from '../types';

const API_URL = import.meta.env.VITE_API_URL || '';

interface AccountSettingsModalProps {
  account: TelegramAccount;
  onClose: () => void;
  onSave: (hasOpenAIToken: boolean) => void;
}

export function AccountSettingsModal({ account, onClose, onSave }: AccountSettingsModalProps) {
  const [openAIToken, setOpenAIToken] = useState('');
  const [proxyUrl, setProxyUrl] = useState('');
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [isTesting, setIsTesting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [proxyTestResult, setProxyTestResult] = useState<{ success: boolean; error?: string } | null>(null);
  const [showToken, setShowToken] = useState(false);

  // Fetch current settings
  useEffect(() => {
    const fetchSettings = async () => {
      try {
        const response = await fetch(`${API_URL}/api/accounts/${account.id}/settings`, {
          credentials: 'include',
        });

        if (response.ok) {
          const data = await response.json();
          setOpenAIToken(data.openai_token || '');
          setProxyUrl(data.proxy_url || '');
        }
      } catch (err) {
        console.error('Failed to fetch settings:', err);
      } finally {
        setIsLoading(false);
      }
    };

    fetchSettings();
  }, [account.id]);

  const handleSave = async () => {
    setError(null);
    setIsSaving(true);

    try {
      const response = await fetch(`${API_URL}/api/accounts/${account.id}/settings`, {
        method: 'PUT',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          openai_token: openAIToken.trim(),
          proxy_url: proxyUrl.trim(),
        }),
      });

      if (!response.ok) {
        const data = await response.json();
        throw new Error(data.error || 'Failed to save settings');
      }

      onSave(openAIToken.trim().length > 0);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save settings');
    } finally {
      setIsSaving(false);
    }
  };

  const handleTestProxy = async () => {
    if (!proxyUrl.trim()) {
      setProxyTestResult({ success: false, error: 'Please enter a proxy URL first' });
      return;
    }

    setIsTesting(true);
    setProxyTestResult(null);

    try {
      const response = await fetch(`${API_URL}/api/accounts/${account.id}/test-proxy`, {
        method: 'POST',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          proxy_url: proxyUrl.trim(),
        }),
      });

      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || 'Failed to test proxy');
      }

      setProxyTestResult(data);
    } catch (err) {
      setProxyTestResult({
        success: false,
        error: err instanceof Error ? err.message : 'Failed to test proxy',
      });
    } finally {
      setIsTesting(false);
    }
  };

  const handleClearToken = () => {
    setOpenAIToken('');
  };

  const handleClearProxy = () => {
    setProxyUrl('');
    setProxyTestResult(null);
  };

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal account-settings-modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <h2>Account Settings</h2>
          <button className="modal-close" onClick={onClose}>
            &times;
          </button>
        </div>

        <div className="modal-content">
          <div className="settings-account-info">
            <span className="settings-account-name">
              {account.first_name} {account.last_name}
            </span>
            {account.username && (
              <span className="settings-account-username">@{account.username}</span>
            )}
          </div>

          {isLoading ? (
            <div className="settings-loading">
              <div className="loading-spinner small" />
              <span>Loading settings...</span>
            </div>
          ) : (
            <>
              {/* Proxy Configuration Section */}
              <div className="form-group">
                <label htmlFor="proxy-url">Proxy Configuration</label>
                <p className="form-hint">
                  Configure a proxy (SOCKS5 or HTTP) for all Telegram API calls from this account.
                  Format: <code>socks5://user:pass@host:port</code> or <code>http://host:port</code>
                </p>
                <div className="proxy-input-wrapper">
                  <input
                    id="proxy-url"
                    type="text"
                    value={proxyUrl}
                    onChange={(e) => {
                      setProxyUrl(e.target.value);
                      setProxyTestResult(null);
                    }}
                    placeholder="socks5://host:port"
                    className="proxy-input"
                    disabled={isSaving || isTesting}
                  />
                  {proxyUrl && (
                    <button
                      type="button"
                      className="proxy-clear-btn"
                      onClick={handleClearProxy}
                      title="Clear proxy"
                    >
                      &times;
                    </button>
                  )}
                </div>
                <div className="proxy-test-section">
                  <button
                    type="button"
                    className="btn-secondary btn-small"
                    onClick={handleTestProxy}
                    disabled={isTesting || !proxyUrl.trim()}
                  >
                    {isTesting ? (
                      <>
                        <div className="loading-spinner small" />
                        <span>Testing...</span>
                      </>
                    ) : (
                      'Test Connection'
                    )}
                  </button>
                  {proxyTestResult && (
                    <span className={`proxy-test-result ${proxyTestResult.success ? 'success' : 'error'}`}>
                      {proxyTestResult.success ? (
                        <>
                          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                            <polyline points="20 6 9 17 4 12"></polyline>
                          </svg>
                          Connection successful
                        </>
                      ) : (
                        <>
                          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                            <circle cx="12" cy="12" r="10"></circle>
                            <line x1="15" y1="9" x2="9" y2="15"></line>
                            <line x1="9" y1="9" x2="15" y2="15"></line>
                          </svg>
                          {proxyTestResult.error}
                        </>
                      )}
                    </span>
                  )}
                </div>
              </div>

              <div className="settings-divider" />

              {/* OpenAI Token Section */}
              <div className="form-group">
                <label htmlFor="openai-token">OpenAI API Token</label>
                <p className="form-hint">
                  Provide your OpenAI API token to enable AI-powered message rewriting. 
                  The token will be used to personalize messages before sending.
                </p>
                <div className="token-input-wrapper">
                  <input
                    id="openai-token"
                    type={showToken ? 'text' : 'password'}
                    value={openAIToken}
                    onChange={(e) => setOpenAIToken(e.target.value)}
                    placeholder="sk-..."
                    className="token-input"
                    disabled={isSaving}
                  />
                  <div className="token-input-actions">
                    <button
                      type="button"
                      className="token-toggle-btn"
                      onClick={() => setShowToken(!showToken)}
                      title={showToken ? 'Hide token' : 'Show token'}
                    >
                      {showToken ? (
                        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                          <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"></path>
                          <line x1="1" y1="1" x2="23" y2="23"></line>
                        </svg>
                      ) : (
                        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                          <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path>
                          <circle cx="12" cy="12" r="3"></circle>
                        </svg>
                      )}
                    </button>
                    {openAIToken && (
                      <button
                        type="button"
                        className="token-clear-btn"
                        onClick={handleClearToken}
                        title="Clear token"
                      >
                        &times;
                      </button>
                    )}
                  </div>
                </div>
              </div>

              {error && (
                <div className="error-message">{error}</div>
              )}
            </>
          )}
        </div>

        <div className="modal-actions">
          <button
            className="btn-secondary"
            onClick={onClose}
            disabled={isSaving}
          >
            Cancel
          </button>
          <button
            className="btn-primary"
            onClick={handleSave}
            disabled={isLoading || isSaving}
          >
            {isSaving ? (
              <>
                <div className="loading-spinner small" />
                <span>Saving...</span>
              </>
            ) : (
              'Save Settings'
            )}
          </button>
        </div>
      </div>
    </div>
  );
}

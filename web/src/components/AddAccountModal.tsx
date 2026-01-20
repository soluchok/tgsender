import { useEffect, useState } from 'react';
import { useAccounts } from '../contexts';

interface AddAccountModalProps {
  onClose: () => void;
}

export function AddAccountModal({ onClose }: AddAccountModalProps) {
  const { qrAuth, startQRAuth, cancelQRAuth, submitPassword } = useAccounts();
  const [password, setPassword] = useState('');
  const [isSubmitting, setIsSubmitting] = useState(false);

  useEffect(() => {
    // Start QR auth when modal opens
    if (qrAuth.status === 'idle') {
      startQRAuth();
    }

    // Cleanup on unmount - always cancel to stop polling
    return () => {
      cancelQRAuth();
    };
  }, []);

  useEffect(() => {
    // Close modal on success after a short delay
    if (qrAuth.status === 'success') {
      const timeout = setTimeout(() => {
        onClose();
        cancelQRAuth();
      }, 1500);
      return () => clearTimeout(timeout);
    }
  }, [qrAuth.status, onClose, cancelQRAuth]);

  const handleClose = () => {
    cancelQRAuth();
    onClose();
  };

  const handleRetry = () => {
    setPassword('');
    startQRAuth();
  };

  const handlePasswordSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!password.trim() || isSubmitting) return;

    setIsSubmitting(true);
    try {
      await submitPassword(password);
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="modal-overlay" onClick={handleClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <h2>Add Telegram Account</h2>
          <button className="modal-close" onClick={handleClose}>
            &times;
          </button>
        </div>

        <div className="modal-content">
          {qrAuth.status === 'pending' && (
            <div className="qr-loading">
              <div className="loading-spinner" />
              <p>Generating QR code...</p>
            </div>
          )}

          {qrAuth.status === 'scanning' && qrAuth.qr_url && (
            <div className="qr-container">
              <div className="qr-code">
                <img src={qrAuth.qr_url} alt="Scan this QR code with Telegram" />
              </div>
              <div className="qr-instructions">
                <h3>Scan with Telegram</h3>
                <ol>
                  <li>Open Telegram on your phone</li>
                  <li>Go to <strong>Settings &gt; Devices &gt; Link Desktop Device</strong></li>
                  <li>Point your phone at this QR code</li>
                </ol>
              </div>
              <div className="qr-waiting">
                <div className="loading-spinner small" />
                <span>Waiting for scan...</span>
              </div>
            </div>
          )}

          {qrAuth.status === 'password_required' && (
            <div className="password-container">
              <div className="password-icon">
                <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                  <rect x="3" y="11" width="18" height="11" rx="2" ry="2"/>
                  <path d="M7 11V7a5 5 0 0 1 10 0v4"/>
                </svg>
              </div>
              <h3>Two-Factor Authentication</h3>
              <p>This account has 2FA enabled. Please enter your password to continue.</p>
              
              {qrAuth.error && (
                <div className="password-error">
                  {qrAuth.error}
                </div>
              )}

              <form onSubmit={handlePasswordSubmit} className="password-form">
                <input
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="Enter your 2FA password"
                  className="password-input"
                  autoFocus
                  disabled={isSubmitting}
                />
                <button 
                  type="submit" 
                  className="password-submit-btn"
                  disabled={!password.trim() || isSubmitting}
                >
                  {isSubmitting ? (
                    <>
                      <div className="loading-spinner small" />
                      <span>Verifying...</span>
                    </>
                  ) : (
                    'Submit'
                  )}
                </button>
              </form>

              <button className="back-btn" onClick={handleRetry} disabled={isSubmitting}>
                Start Over
              </button>
            </div>
          )}

          {qrAuth.status === 'success' && (
            <div className="qr-success">
              <div className="success-icon">&#10003;</div>
              <h3>Account Added!</h3>
              <p>Your Telegram account has been linked successfully.</p>
            </div>
          )}

          {qrAuth.status === 'error' && (
            <div className="qr-error">
              <div className="error-icon">!</div>
              <h3>Something went wrong</h3>
              <p>{qrAuth.error || 'Failed to authenticate. Please try again.'}</p>
              <button className="retry-btn" onClick={handleRetry}>
                Try Again
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

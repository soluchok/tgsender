import { Navigate } from 'react-router-dom';
import { TelegramLoginButton } from '../components';
import { useAuth } from '../contexts';

// Replace with your Telegram bot username (without @)
const TELEGRAM_BOT_NAME = import.meta.env.VITE_TELEGRAM_BOT_NAME || 'your_bot_name';

export function LoginPage() {
  const { isAuthenticated, isLoading, error } = useAuth();

  if (isLoading) {
    return (
      <div className="page login-page">
        <div className="loading-container">
          <div className="loading-spinner" />
          <p>Loading...</p>
        </div>
      </div>
    );
  }

  if (isAuthenticated) {
    return <Navigate to="/dashboard" replace />;
  }

  return (
    <div className="page login-page">
      <div className="login-container">
        <div className="login-header">
          <h1>TG Sender</h1>
          <p>Sign in with your Telegram account to continue</p>
        </div>

        {error && (
          <div className="error-message">
            {error}
          </div>
        )}

        <div className="login-button-wrapper">
          <TelegramLoginButton
            botName={TELEGRAM_BOT_NAME}
            buttonSize="large"
            cornerRadius={8}
            requestAccess="write"
            showUserPhoto={true}
          />
        </div>

        <div className="login-footer">
          <p>
            By signing in, you agree to our terms of service and privacy policy.
          </p>
        </div>
      </div>
    </div>
  );
}

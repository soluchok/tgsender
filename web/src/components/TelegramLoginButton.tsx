import { useEffect, useRef, useCallback } from 'react';
import { TelegramUser } from '../types';
import { useAuth } from '../contexts';

declare global {
  interface Window {
    TelegramLoginWidget: {
      dataOnauth: (user: TelegramUser) => void;
    };
  }
}

interface TelegramLoginButtonProps {
  botName: string;
  buttonSize?: 'large' | 'medium' | 'small';
  cornerRadius?: number;
  requestAccess?: 'write';
  showUserPhoto?: boolean;
  lang?: string;
}

export function TelegramLoginButton({
  botName,
  buttonSize = 'large',
  cornerRadius = 8,
  requestAccess = 'write',
  showUserPhoto = true,
  lang = 'en',
}: TelegramLoginButtonProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const { login, isLoading } = useAuth();

  const handleAuth = useCallback(
    async (user: TelegramUser) => {
      try {
        await login(user);
      } catch (error) {
        console.error('Telegram auth failed:', error);
      }
    },
    [login]
  );

  useEffect(() => {
    // Set up the callback function
    window.TelegramLoginWidget = {
      dataOnauth: handleAuth,
    };

    // Create the script element
    const script = document.createElement('script');
    script.src = 'https://telegram.org/js/telegram-widget.js?22';
    script.async = true;
    script.setAttribute('data-telegram-login', botName);
    script.setAttribute('data-size', buttonSize);
    script.setAttribute('data-radius', cornerRadius.toString());
    script.setAttribute('data-request-access', requestAccess);
    script.setAttribute('data-userpic', showUserPhoto.toString());
    script.setAttribute('data-lang', lang);
    script.setAttribute('data-onauth', 'TelegramLoginWidget.dataOnauth(user)');

    // Clear container and append script
    if (containerRef.current) {
      containerRef.current.innerHTML = '';
      containerRef.current.appendChild(script);
    }

    return () => {
      // Cleanup
      if (containerRef.current) {
        containerRef.current.innerHTML = '';
      }
    };
  }, [botName, buttonSize, cornerRadius, requestAccess, showUserPhoto, lang, handleAuth]);

  if (isLoading) {
    return <div className="telegram-login-loading">Loading...</div>;
  }

  return <div ref={containerRef} className="telegram-login-container" />;
}

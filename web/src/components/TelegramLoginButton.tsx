import { useEffect, useRef } from 'react';
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

// Parse Telegram auth data from URL hash or search params
function parseTelegramAuthFromURL(): TelegramUser | null {
  const params = new URLSearchParams(window.location.search);
  
  const id = params.get('id');
  const first_name = params.get('first_name');
  const auth_date = params.get('auth_date');
  const hash = params.get('hash');
  
  if (!id || !first_name || !auth_date || !hash) {
    return null;
  }
  
  return {
    id: parseInt(id, 10),
    first_name,
    last_name: params.get('last_name') || undefined,
    username: params.get('username') || undefined,
    photo_url: params.get('photo_url') || undefined,
    auth_date: parseInt(auth_date, 10),
    hash,
  };
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
  
  // Use ref to always have access to the latest login function
  const loginRef = useRef(login);
  loginRef.current = login;

  // Check for redirect-based auth data in URL on mount
  useEffect(() => {
    const user = parseTelegramAuthFromURL();
    if (user) {
      console.log('Found Telegram auth data in URL:', user);
      // Clean URL params
      window.history.replaceState({}, document.title, window.location.pathname);
      // Process login
      loginRef.current(user).then(() => {
        console.log('Login from redirect completed');
      }).catch((error) => {
        console.error('Login from redirect failed:', error);
      });
    }
  }, []);

  useEffect(() => {
    // Set up the callback function for popup mode (fallback)
    window.TelegramLoginWidget = {
      dataOnauth: async (user: TelegramUser) => {
        console.log('Telegram auth callback received:', user);
        try {
          await loginRef.current(user);
          console.log('Login completed successfully');
        } catch (error) {
          console.error('Telegram auth failed:', error);
        }
      },
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
    // Set auth-url for redirect mode - redirect back to current page
    script.setAttribute('data-auth-url', window.location.origin + '/login');

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
  }, [botName, buttonSize, cornerRadius, requestAccess, showUserPhoto, lang]);

  if (isLoading) {
    return <div className="telegram-login-loading">Loading...</div>;
  }

  return <div ref={containerRef} className="telegram-login-container" />;
}

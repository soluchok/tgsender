import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAccounts } from '../contexts';
import { TelegramAccount } from '../types';
import { AddAccountModal } from './AddAccountModal';

export function Sidebar() {
  const { accounts, selectedAccount, isLoading } = useAccounts();
  const navigate = useNavigate();
  const [showAddModal, setShowAddModal] = useState(false);

  const handleSelectAccount = (account: TelegramAccount) => {
    navigate(`/dashboard/${account.id}`);
  };

  return (
    <>
      <aside className="sidebar">
        <div className="sidebar-header">
          <h2>Telegram Accounts</h2>
          <button
            className="add-account-btn"
            onClick={() => setShowAddModal(true)}
            title="Add Account"
          >
            +
          </button>
        </div>

        <div className="accounts-list">
          {isLoading ? (
            <div className="sidebar-loading">
              <div className="loading-spinner small" />
              <span>Loading...</span>
            </div>
          ) : accounts.length === 0 ? (
            <div className="no-accounts">
              <p>No accounts added yet</p>
              <button
                className="add-first-account-btn"
                onClick={() => setShowAddModal(true)}
              >
                Add your first account
              </button>
            </div>
          ) : (
            accounts.map((account) => (
              <AccountItem
                key={account.id}
                account={account}
                isSelected={selectedAccount?.id === account.id}
                onSelect={() => handleSelectAccount(account)}
              />
            ))
          )}
        </div>

        <div className="sidebar-footer">
          <a
            href="https://github.com/soluchok/tgsender"
            target="_blank"
            rel="noopener noreferrer"
            className="github-link"
            title="View on GitHub"
          >
            <svg viewBox="0 0 24 24" width="20" height="20" fill="currentColor">
              <path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0024 12c0-6.63-5.37-12-12-12z"/>
            </svg>
            <span>GitHub</span>
          </a>
        </div>
      </aside>

      {showAddModal && (
        <AddAccountModal onClose={() => setShowAddModal(false)} />
      )}
    </>
  );
}

interface AccountItemProps {
  account: TelegramAccount;
  isSelected: boolean;
  onSelect: () => void;
}

function AccountItem({ account, isSelected, onSelect }: AccountItemProps) {
  const { removeAccount } = useAccounts();
  const navigate = useNavigate();
  const [showMenu, setShowMenu] = useState(false);

  const displayName = [account.first_name, account.last_name]
    .filter(Boolean)
    .join(' ');

  const handleRemove = async (e: React.MouseEvent) => {
    e.stopPropagation();
    if (window.confirm(`Remove account "${displayName}"?`)) {
      await removeAccount(account.id);
      if (isSelected) {
        navigate('/dashboard', { replace: true });
      }
    }
    setShowMenu(false);
  };

  return (
    <div
      className={`account-item ${isSelected ? 'selected' : ''} ${!account.is_active ? 'inactive' : ''}`}
      onClick={onSelect}
    >
      <div className="account-avatar">
        {account.photo_url ? (
          <img src={account.photo_url} alt={displayName} />
        ) : (
          <div className="avatar-placeholder">
            {account.first_name.charAt(0).toUpperCase()}
          </div>
        )}
        <span className={`status-dot ${account.is_active ? 'active' : 'inactive'}`} />
      </div>

      <div className="account-info">
        <span className="account-name">{displayName}</span>
        {account.username && (
          <span className="account-username">@{account.username}</span>
        )}
        <span className="account-phone">{account.phone}</span>
      </div>

      <div className="account-actions">
        <button
          className="menu-btn"
          onClick={(e) => {
            e.stopPropagation();
            setShowMenu(!showMenu);
          }}
        >
          &#8942;
        </button>
        {showMenu && (
          <div className="account-menu">
            <button onClick={handleRemove} className="menu-item danger">
              Remove
            </button>
          </div>
        )}
      </div>
    </div>
  );
}

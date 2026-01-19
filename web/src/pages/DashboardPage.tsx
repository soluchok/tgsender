import { useState, useEffect, useCallback } from 'react';
import { UserProfile, Sidebar, CheckNumbersModal, SendMessagesModal } from '../components';
import { useAuth, useAccounts, AccountsProvider } from '../contexts';
import { Contact } from '../types';

const API_URL = import.meta.env.VITE_API_URL || '';

function DashboardContent() {
  const { user } = useAuth();
  const { selectedAccount, accounts } = useAccounts();
  const [showCheckNumbers, setShowCheckNumbers] = useState(false);
  const [showSendMessages, setShowSendMessages] = useState(false);
  const [contacts, setContacts] = useState<Contact[]>([]);
  const [isLoadingContacts, setIsLoadingContacts] = useState(false);

  const fetchContacts = useCallback(async () => {
    if (!selectedAccount) {
      setContacts([]);
      return;
    }

    setIsLoadingContacts(true);
    try {
      const response = await fetch(`${API_URL}/api/accounts/${selectedAccount.id}/contacts?valid=true`, {
        credentials: 'include',
      });

      if (response.ok) {
        const data = await response.json();
        setContacts(data.contacts || []);
      }
    } catch (err) {
      console.error('Failed to fetch contacts:', err);
    } finally {
      setIsLoadingContacts(false);
    }
  }, [selectedAccount]);

  useEffect(() => {
    fetchContacts();
  }, [fetchContacts]);

  const handleCheckNumbersClose = () => {
    setShowCheckNumbers(false);
    // Refresh contacts after checking numbers
    fetchContacts();
  };

  const handleSendMessagesClose = () => {
    setShowSendMessages(false);
  };

  const handleDeleteContact = async (contactId: string) => {
    if (!confirm('Are you sure you want to delete this contact?')) {
      return;
    }

    try {
      const response = await fetch(`${API_URL}/api/contacts/${contactId}`, {
        method: 'DELETE',
        credentials: 'include',
      });

      if (response.ok) {
        setContacts(prev => prev.filter(c => c.id !== contactId));
      }
    } catch (err) {
      console.error('Failed to delete contact:', err);
    }
  };

  return (
    <div className="dashboard-layout">
      <Sidebar />

      <div className="dashboard-main">
        <header className="dashboard-header">
          <h1>TG Sender Dashboard</h1>
          <UserProfile />
        </header>

        <main className="dashboard-content">
          {selectedAccount ? (
            <div className="account-dashboard">
              <div className="selected-account-header">
                <h2>
                  {selectedAccount.first_name} {selectedAccount.last_name}
                </h2>
                {selectedAccount.username && (
                  <span className="username">@{selectedAccount.username}</span>
                )}
                <span className={`status-badge ${selectedAccount.is_active ? 'active' : 'inactive'}`}>
                  {selectedAccount.is_active ? 'Active' : 'Inactive'}
                </span>
              </div>

              <div className="dashboard-cards">
                <div className="card">
                  <h3>Send Messages</h3>
                  <p>Send messages using this account</p>
                  <button 
                    className="card-button"
                    onClick={() => setShowSendMessages(true)}
                  >
                    Open
                  </button>
                </div>

                <div className="card">
                  <h3>Check Numbers</h3>
                  <p>Check phone numbers on Telegram</p>
                  <button 
                    className="card-button"
                    onClick={() => setShowCheckNumbers(true)}
                  >
                    Open
                  </button>
                </div>

                <div className="card">
                  <h3>Dump Contacts</h3>
                  <p>Export contacts from this account</p>
                  <button className="card-button">Open</button>
                </div>

                <div className="card">
                  <h3>Account Settings</h3>
                  <p>Manage this account's settings</p>
                  <button className="card-button">Open</button>
                </div>
              </div>

              {/* Contacts Section */}
              <div className="contacts-section">
                <div className="contacts-header">
                  <h3>Saved Contacts</h3>
                  <span className="contacts-count">{contacts.length} contacts</span>
                </div>

                {isLoadingContacts ? (
                  <div className="contacts-loading">
                    <div className="loading-spinner small" />
                    <span>Loading contacts...</span>
                  </div>
                ) : contacts.length === 0 ? (
                  <div className="contacts-empty">
                    <p>No contacts saved yet.</p>
                    <p className="hint">Use "Check Numbers" to verify and save contacts.</p>
                  </div>
                ) : (
                  <div className="contacts-grid">
                    {contacts.map((contact) => (
                      <ContactCard 
                        key={contact.id} 
                        contact={contact} 
                        onDelete={() => handleDeleteContact(contact.id)}
                      />
                    ))}
                  </div>
                )}
              </div>
            </div>
          ) : (
            <div className="welcome-section">
              <h2>Welcome, {user?.first_name}!</h2>
              {accounts.length === 0 ? (
                <p>
                  Get started by adding your first Telegram account using the sidebar.
                </p>
              ) : (
                <p>
                  Select a Telegram account from the sidebar to get started.
                </p>
              )}
            </div>
          )}
        </main>
      </div>

      {showCheckNumbers && selectedAccount && (
        <CheckNumbersModal
          account={selectedAccount}
          onClose={handleCheckNumbersClose}
        />
      )}

      {showSendMessages && selectedAccount && (
        <SendMessagesModal
          account={selectedAccount}
          contacts={contacts}
          onClose={handleSendMessagesClose}
        />
      )}
    </div>
  );
}

interface ContactCardProps {
  contact: Contact;
  onDelete: () => void;
}

function ContactCard({ contact, onDelete }: ContactCardProps) {
  const displayName = [contact.first_name, contact.last_name].filter(Boolean).join(' ') || 'Unknown';
  const initial = (contact.first_name || contact.phone || 'U').charAt(0).toUpperCase();

  return (
    <div className="contact-card">
      <div className="contact-card-avatar">
        <div className="avatar-placeholder">
          {initial}
        </div>
      </div>
      <div className="contact-card-info">
        <span className="contact-card-name">{displayName}</span>
        {contact.username && (
          <span className="contact-card-username">@{contact.username}</span>
        )}
        <span className="contact-card-phone">{contact.phone}</span>
      </div>
      <button 
        className="contact-card-delete" 
        onClick={onDelete}
        title="Delete contact"
      >
        &times;
      </button>
    </div>
  );
}

export function DashboardPage() {
  return (
    <AccountsProvider>
      <div className="page dashboard-page">
        <DashboardContent />
      </div>
    </AccountsProvider>
  );
}

import { useState, useEffect, useCallback, useRef } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { UserProfile, Sidebar, CheckNumbersModal, SendMessagesModal, EditContactModal, AccountSettingsModal, ExportContactsModal, ImportContactsModal } from '../components';
import { useAuth, useAccounts, AccountsProvider } from '../contexts';
import { Contact } from '../types';

const API_URL = import.meta.env.VITE_API_URL || '';

interface ImportProgress {
  progress: number;
  imported: number;
  skipped: number;
  status: 'pending' | 'running' | 'completed' | 'failed';
  error?: string;
  importType?: 'chats' | 'contacts';
}

function DashboardContent() {
  const { user } = useAuth();
  const { selectedAccount, accounts, selectAccount, updateAccount } = useAccounts();
  const { accountId } = useParams<{ accountId?: string }>();
  const navigate = useNavigate();
  const [showCheckNumbers, setShowCheckNumbers] = useState(false);
  const [showSendMessages, setShowSendMessages] = useState(false);
  const [showAccountSettings, setShowAccountSettings] = useState(false);
  const [showExportContacts, setShowExportContacts] = useState(false);
  const [showImportContacts, setShowImportContacts] = useState(false);
  const [editingContact, setEditingContact] = useState<Contact | null>(null);
  const [contacts, setContacts] = useState<Contact[]>([]);
  const [contactSearch, setContactSearch] = useState('');
  const [isLoadingContacts, setIsLoadingContacts] = useState(false);
  const [importProgress, setImportProgress] = useState<ImportProgress | null>(null);
  const pollIntervalRef = useRef<number | null>(null);

  // Sync URL with selected account
  useEffect(() => {
    if (accounts.length === 0) return;

    // If URL has accountId, select that account
    if (accountId) {
      const account = accounts.find(a => a.id === accountId);
      if (account && selectedAccount?.id !== accountId) {
        selectAccount(account);
      } else if (!account) {
        // Account not found, redirect to dashboard
        navigate('/dashboard', { replace: true });
      }
    } else if (selectedAccount) {
      // If account is selected but URL doesn't have it, update URL
      navigate(`/dashboard/${selectedAccount.id}`, { replace: true });
    }
  }, [accountId, accounts, selectedAccount?.id, selectAccount, navigate]);

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
  }, [selectedAccount?.id]);

  useEffect(() => {
    fetchContacts();
  }, [selectedAccount?.id]);

  // Clear import progress when switching accounts
  useEffect(() => {
    if (pollIntervalRef.current) {
      clearInterval(pollIntervalRef.current);
      pollIntervalRef.current = null;
    }
    setImportProgress(null);

    // Check for active import job when switching accounts
    if (selectedAccount) {
      checkForActiveImportJob(selectedAccount.id);
    }
  }, [selectedAccount?.id]);

  // Start polling for a specific job
  const startPolling = useCallback((accountId: string, jobId: string) => {
    // Clear any existing poll interval
    if (pollIntervalRef.current) {
      clearInterval(pollIntervalRef.current);
      pollIntervalRef.current = null;
    }

    pollIntervalRef.current = window.setInterval(async () => {
      try {
        const statusResponse = await fetch(
          `${API_URL}/api/accounts/${accountId}/import-chats/status?job_id=${jobId}`,
          { credentials: 'include' }
        );

        const statusData = await statusResponse.json();

        if (!statusResponse.ok) {
          throw new Error(statusData.error || 'Failed to get status');
        }

        setImportProgress({
          progress: statusData.progress,
          imported: statusData.imported,
          skipped: statusData.skipped,
          status: statusData.status,
          error: statusData.error,
        });

        // Stop polling if job is done
        if (statusData.status === 'completed' || statusData.status === 'failed') {
          if (pollIntervalRef.current) {
            clearInterval(pollIntervalRef.current);
            pollIntervalRef.current = null;
          }

          // Refresh contacts after completion
          if (statusData.status === 'completed') {
            fetchContacts();
          }

          // Clear progress after a delay
          setTimeout(() => {
            setImportProgress(null);
          }, 5000);
        }
      } catch (err) {
        console.error('Failed to poll status:', err);
        if (pollIntervalRef.current) {
          clearInterval(pollIntervalRef.current);
          pollIntervalRef.current = null;
        }
        setImportProgress(prev => prev ? { ...prev, status: 'failed', error: 'Failed to get status' } : null);
      }
    }, 1000);
  }, [fetchContacts]);

  // Check if there's an active import job for the account
  const checkForActiveImportJob = async (accountId: string) => {
    try {
      const response = await fetch(
        `${API_URL}/api/accounts/${accountId}/import-chats/status`,
        { credentials: 'include' }
      );

      if (!response.ok) return;

      const data = await response.json();

      if (data.active && (data.status === 'pending' || data.status === 'running')) {
        // There's an active job - set progress and start polling
        setImportProgress({
          progress: data.progress,
          imported: data.imported,
          skipped: data.skipped,
          status: data.status,
          error: data.error,
        });
        startPolling(accountId, data.id);
      }
    } catch (err) {
      console.error('Failed to check for active import job:', err);
    }
  };

  const handleCheckNumbersClose = () => {
    setShowCheckNumbers(false);
    // Refresh contacts after checking numbers
    fetchContacts();
  };

  const handleSendMessagesClose = () => {
    setShowSendMessages(false);
  };

  const handleDeleteContact = async (contactId: string) => {
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

  const handleEditContact = (contact: Contact) => {
    setEditingContact(contact);
  };

  const handleContactUpdated = (updatedContact: Contact) => {
    setContacts(prev => prev.map(c => c.id === updatedContact.id ? updatedContact : c));
  };

  const handleImportFromChats = async () => {
    if (!selectedAccount) return;

    // Clear any existing poll interval
    if (pollIntervalRef.current) {
      clearInterval(pollIntervalRef.current);
      pollIntervalRef.current = null;
    }

    try {
      // Start the import job
      const response = await fetch(`${API_URL}/api/accounts/${selectedAccount.id}/import-chats`, {
        method: 'POST',
        credentials: 'include',
      });

      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || 'Failed to start import');
      }

      // Set initial progress and start polling
      setImportProgress({
        progress: data.progress,
        imported: data.imported,
        skipped: data.skipped,
        status: data.status,
        importType: 'chats',
      });

      startPolling(selectedAccount.id, data.id);
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to import contacts from chats');
      setImportProgress(null);
    }
  };

  const handleImportContacts = async () => {
    if (!selectedAccount) return;

    // Clear any existing poll interval
    if (pollIntervalRef.current) {
      clearInterval(pollIntervalRef.current);
      pollIntervalRef.current = null;
    }

    try {
      // Start the import job
      const response = await fetch(`${API_URL}/api/accounts/${selectedAccount.id}/import-contacts`, {
        method: 'POST',
        credentials: 'include',
      });

      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || 'Failed to start import');
      }

      // Set initial progress and start polling
      setImportProgress({
        progress: data.progress,
        imported: data.imported,
        skipped: data.skipped,
        status: data.status,
        importType: 'contacts',
      });

      startPolling(selectedAccount.id, data.id);
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to import contacts');
      setImportProgress(null);
    }
  };

  // Cleanup poll interval on unmount or account change
  useEffect(() => {
    return () => {
      if (pollIntervalRef.current) {
        clearInterval(pollIntervalRef.current);
      }
    };
  }, [selectedAccount?.id]);

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
                  <h3>Export Contacts</h3>
                  <p>Export contacts to a JSON file</p>
                  <button 
                    className="card-button"
                    onClick={() => setShowExportContacts(true)}
                  >
                    Open
                  </button>
                </div>

                <div className="card">
                  <h3>Import Contacts</h3>
                  <p>Import contacts from a JSON file</p>
                  <button 
                    className="card-button"
                    onClick={() => setShowImportContacts(true)}
                  >
                    Open
                  </button>
                </div>

                <div className="card">
                  <h3>Account Settings</h3>
                  <p>Manage this account's settings</p>
                  <button 
                    className="card-button"
                    onClick={() => setShowAccountSettings(true)}
                  >
                    Open
                  </button>
                </div>
              </div>

              {/* Contacts Section */}
              <div className="contacts-section">
                <div className="contacts-header">
                  <h3>Contacts</h3>
                  {contacts.length > 0 && (
                    <div className="contacts-search">
                      <input
                        type="text"
                        className="contacts-search-input"
                        placeholder="Search contacts..."
                        value={contactSearch}
                        onChange={(e) => setContactSearch(e.target.value)}
                      />
                      {contactSearch && (
                        <button
                          className="contacts-search-clear"
                          onClick={() => setContactSearch('')}
                          title="Clear search"
                        >
                          &times;
                        </button>
                      )}
                    </div>
                  )}
                  <div className="contacts-header-actions">
                    <span className="contacts-count">{contacts.length} contacts</span>
                    {importProgress && (
                      <span className={`import-progress ${importProgress.status}`}>
                        {importProgress.status === 'running' || importProgress.status === 'pending' ? (
                          <>Processing: {importProgress.progress} dialogs, {importProgress.imported} new, {importProgress.skipped} skipped</>
                        ) : importProgress.status === 'completed' ? (
                          <>Done: {importProgress.imported} imported, {importProgress.skipped} skipped</>
                        ) : (
                          <>Failed: {importProgress.error || 'Unknown error'}</>
                        )}
                      </span>
                    )}
                    <button
                      className="add-contact-btn"
                      onClick={handleImportFromChats}
                      disabled={importProgress !== null && (importProgress.status === 'pending' || importProgress.status === 'running')}
                      title="Import from chats"
                    >
                      {importProgress && importProgress.importType === 'chats' && (importProgress.status === 'pending' || importProgress.status === 'running') ? (
                        <div className="loading-spinner small" />
                      ) : (
                        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                          <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
                          <polyline points="7 10 12 15 17 10"></polyline>
                          <line x1="12" y1="15" x2="12" y2="3"></line>
                        </svg>
                      )}
                    </button>
                    <button
                      className="add-contact-btn"
                      onClick={handleImportContacts}
                      disabled={importProgress !== null && (importProgress.status === 'pending' || importProgress.status === 'running')}
                      title="Import contacts"
                    >
                      {importProgress && importProgress.importType === 'contacts' && (importProgress.status === 'pending' || importProgress.status === 'running') ? (
                        <div className="loading-spinner small" />
                      ) : (
                        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                          <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"></path>
                          <circle cx="9" cy="7" r="4"></circle>
                          <path d="M23 21v-2a4 4 0 0 0-3-3.87"></path>
                          <path d="M16 3.13a4 4 0 0 1 0 7.75"></path>
                        </svg>
                      )}
                    </button>
                    <button
                      className="add-contact-btn"
                      onClick={() => setShowCheckNumbers(true)}
                      title="Add contacts"
                    >
                      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                        <line x1="12" y1="5" x2="12" y2="19"></line>
                        <line x1="5" y1="12" x2="19" y2="12"></line>
                      </svg>
                    </button>
                  </div>
                </div>

                {isLoadingContacts ? (
                  <div className="contacts-loading">
                    <div className="loading-spinner small" />
                    <span>Loading contacts...</span>
                  </div>
                ) : contacts.length === 0 ? (
                  <div className="contacts-empty">
                    <p>No contacts saved yet.</p>
                    <p className="hint">Click the + button to add contacts.</p>
                  </div>
                ) : (
                  (() => {
                    const searchLower = contactSearch.toLowerCase().trim().replace(/^@/, '');
                    const filteredContacts = searchLower
                      ? contacts.filter(c => {
                          const fullName = [c.first_name, c.last_name].filter(Boolean).join(' ').toLowerCase() || 'unknown';
                          return fullName.includes(searchLower) ||
                            (c.username && c.username.toLowerCase().includes(searchLower)) ||
                            (c.phone && c.phone.includes(searchLower)) ||
                            (c.labels && c.labels.some(l => l.toLowerCase().includes(searchLower)));
                        })
                      : contacts;

                    return filteredContacts.length === 0 ? (
                      <div className="contacts-empty">
                        <p>No contacts match your search.</p>
                      </div>
                    ) : (
                      <div className="contacts-grid">
                        {filteredContacts.map((contact) => (
                          <ContactCard
                            key={contact.id}
                            contact={contact}
                            onDelete={() => handleDeleteContact(contact.id)}
                            onEdit={() => handleEditContact(contact)}
                          />
                        ))}
                      </div>
                    );
                  })()
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

      {showAccountSettings && selectedAccount && (
        <AccountSettingsModal
          account={selectedAccount}
          onClose={() => setShowAccountSettings(false)}
          onSave={(hasOpenAIToken) => {
            if (updateAccount) {
              updateAccount({ ...selectedAccount, has_openai_token: hasOpenAIToken });
            }
          }}
        />
      )}

      {editingContact && (
        <EditContactModal
          contact={editingContact}
          onClose={() => setEditingContact(null)}
          onSave={handleContactUpdated}
        />
      )}

      {showExportContacts && selectedAccount && (
        <ExportContactsModal
          contacts={contacts}
          onClose={() => setShowExportContacts(false)}
        />
      )}

      {showImportContacts && selectedAccount && (
        <ImportContactsModal
          account={selectedAccount}
          onClose={() => setShowImportContacts(false)}
          onImported={fetchContacts}
        />
      )}
    </div>
  );
}

interface ContactCardProps {
  contact: Contact;
  onDelete: () => void;
  onEdit: () => void;
}

function ContactCard({ contact, onDelete, onEdit }: ContactCardProps) {
  const displayName = [contact.first_name, contact.last_name].filter(Boolean).join(' ') || 'Unknown';
  const initial = (contact.first_name || contact.phone || 'U').charAt(0).toUpperCase();

  return (
    <div className="contact-card">
      <div className="contact-card-avatar">
        {contact.photo_url ? (
          <img src={contact.photo_url} alt={displayName} className="avatar-image" />
        ) : (
          <div className="avatar-placeholder">
            {initial}
          </div>
        )}
      </div>
      <div className="contact-card-info">
        <div className="contact-card-name-row">
          <span className="contact-card-name">{displayName}</span>
          {contact.labels && contact.labels.length > 0 && (
            <div className="contact-card-labels">
              {contact.labels.map((label, index) => (
                <span key={index} className="contact-label">
                  {label}
                </span>
              ))}
            </div>
          )}
        </div>
        {contact.username && (
          <span className="contact-card-username">@{contact.username}</span>
        )}
        <span className="contact-card-phone">{contact.phone}</span>
      </div>
      <div className="contact-card-actions">
        <button
          className="contact-card-edit"
          onClick={onEdit}
          title="Edit contact"
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"></path>
            <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"></path>
          </svg>
        </button>
        <button
          className="contact-card-delete"
          onClick={onDelete}
          title="Delete contact"
        >
          &times;
        </button>
      </div>
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

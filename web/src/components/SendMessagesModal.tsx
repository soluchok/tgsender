import { useState, useMemo } from 'react';
import { TelegramAccount, Contact } from '../types';

const API_URL = import.meta.env.VITE_API_URL || '';

interface SendMessagesModalProps {
  account: TelegramAccount;
  contacts: Contact[];
  onClose: () => void;
}

interface RecipientResult {
  contact_id: string;
  phone: string;
  name: string;
  success: boolean;
  error?: string;
}

interface SendResult {
  total: number;
  successful: number;
  failed: number;
  results: RecipientResult[];
}

export function SendMessagesModal({ account, contacts, onClose }: SendMessagesModalProps) {
  const [message, setMessage] = useState('');
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set(contacts.map(c => c.id)));
  const [isSending, setIsSending] = useState(false);
  const [result, setResult] = useState<SendResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [selectedLabels, setSelectedLabels] = useState<Set<string>>(new Set());

  // Extract unique labels from all contacts
  const availableLabels = useMemo(() => {
    const labels = new Set<string>();
    contacts.forEach(contact => {
      contact.labels?.forEach(label => labels.add(label));
    });
    return Array.from(labels).sort();
  }, [contacts]);

  // Filter contacts based on selected labels
  const filteredContacts = useMemo(() => {
    if (selectedLabels.size === 0) {
      return contacts;
    }
    return contacts.filter(contact => 
      contact.labels?.some(label => selectedLabels.has(label))
    );
  }, [contacts, selectedLabels]);

  const handleToggleLabel = (label: string) => {
    const newLabels = new Set(selectedLabels);
    if (newLabels.has(label)) {
      newLabels.delete(label);
    } else {
      newLabels.add(label);
    }
    setSelectedLabels(newLabels);
    
    // Update selected contacts to only include filtered contacts
    const filteredIds = new Set(
      contacts
        .filter(c => newLabels.size === 0 || c.labels?.some(l => newLabels.has(l)))
        .map(c => c.id)
    );
    setSelectedIds(filteredIds);
  };

  const handleClearLabelFilter = () => {
    setSelectedLabels(new Set());
    setSelectedIds(new Set(contacts.map(c => c.id)));
  };

  const handleSelectAll = () => {
    if (selectedIds.size === filteredContacts.length) {
      setSelectedIds(new Set());
    } else {
      setSelectedIds(new Set(filteredContacts.map(c => c.id)));
    }
  };

  const handleToggleContact = (contactId: string) => {
    const newSelected = new Set(selectedIds);
    if (newSelected.has(contactId)) {
      newSelected.delete(contactId);
    } else {
      newSelected.add(contactId);
    }
    setSelectedIds(newSelected);
  };

  const handleSend = async () => {
    setError(null);
    setResult(null);

    if (selectedIds.size === 0) {
      setError('Please select at least one contact');
      return;
    }

    if (!message.trim()) {
      setError('Please enter a message');
      return;
    }

    setIsSending(true);

    try {
      const response = await fetch(`${API_URL}/api/accounts/${account.id}/send`, {
        method: 'POST',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          contact_ids: Array.from(selectedIds),
          message: message.trim(),
        }),
      });

      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || 'Failed to send messages');
      }

      setResult(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to send messages');
    } finally {
      setIsSending(false);
    }
  };

  const handleClose = () => {
    onClose();
  };

  const allSelected = selectedIds.size === filteredContacts.length && filteredContacts.length > 0;
  const noneSelected = selectedIds.size === 0;

  return (
    <div className="modal-overlay" onClick={handleClose}>
      <div className="modal send-messages-modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <h2>Send Messages</h2>
          <button className="modal-close" onClick={handleClose}>
            &times;
          </button>
        </div>

        <div className="modal-content">
          <p className="modal-description">
            Send messages using account: <strong>{account.first_name}</strong>
          </p>

          {!result ? (
            <>
              {contacts.length === 0 ? (
                <div className="empty-state">
                  <p>No contacts available.</p>
                  <p className="hint">Use "Check Numbers" to add contacts first.</p>
                </div>
              ) : (
                <>
                  <div className="form-group">
                    <label htmlFor="message">Message</label>
                    <textarea
                      id="message"
                      value={message}
                      onChange={(e) => setMessage(e.target.value)}
                      placeholder="Enter your message..."
                      rows={4}
                      disabled={isSending}
                      className="message-input"
                    />
                  </div>

                  {availableLabels.length > 0 && (
                    <div className="form-group">
                      <label>Filter by Labels</label>
                      <div className="label-filter">
                        {availableLabels.map(label => (
                          <button
                            key={label}
                            className={`label-filter-btn ${selectedLabels.has(label) ? 'active' : ''}`}
                            onClick={() => handleToggleLabel(label)}
                            disabled={isSending}
                          >
                            {label}
                          </button>
                        ))}
                        {selectedLabels.size > 0 && (
                          <button
                            className="label-filter-clear"
                            onClick={handleClearLabelFilter}
                            disabled={isSending}
                          >
                            Clear
                          </button>
                        )}
                      </div>
                    </div>
                  )}

                  <div className="contacts-selection">
                    <div className="selection-header">
                      <label>
                        <input
                          type="checkbox"
                          checked={allSelected}
                          onChange={handleSelectAll}
                          disabled={isSending}
                        />
                        <span>Select All ({filteredContacts.length} contacts)</span>
                      </label>
                      <span className="selected-count">
                        {selectedIds.size} selected
                      </span>
                    </div>

                    <div className="contacts-list selectable">
                      {filteredContacts.map((contact) => (
                        <ContactSelectItem
                          key={contact.id}
                          contact={contact}
                          selected={selectedIds.has(contact.id)}
                          onToggle={() => handleToggleContact(contact.id)}
                          disabled={isSending}
                        />
                      ))}
                    </div>
                  </div>
                </>
              )}

              {error && (
                <div className="error-message">{error}</div>
              )}

              <div className="modal-actions">
                <button
                  className="btn-secondary"
                  onClick={handleClose}
                  disabled={isSending}
                >
                  Cancel
                </button>
                <button
                  className="btn-primary"
                  onClick={handleSend}
                  disabled={isSending || noneSelected || !message.trim() || contacts.length === 0}
                >
                  {isSending ? (
                    <>
                      <div className="loading-spinner small" />
                      <span>Sending...</span>
                    </>
                  ) : (
                    `Send to ${selectedIds.size} Contact${selectedIds.size !== 1 ? 's' : ''}`
                  )}
                </button>
              </div>
            </>
          ) : (
            <SendResultView result={result} onBack={() => setResult(null)} onClose={handleClose} />
          )}
        </div>
      </div>
    </div>
  );
}

interface ContactSelectItemProps {
  contact: Contact;
  selected: boolean;
  onToggle: () => void;
  disabled: boolean;
}

function ContactSelectItem({ contact, selected, onToggle, disabled }: ContactSelectItemProps) {
  const displayName = [contact.first_name, contact.last_name].filter(Boolean).join(' ') || 'Unknown';
  const initial = (contact.first_name || contact.phone || 'U').charAt(0).toUpperCase();

  return (
    <div 
      className={`contact-select-item ${selected ? 'selected' : ''}`}
      onClick={() => !disabled && onToggle()}
    >
      <input
        type="checkbox"
        checked={selected}
        onChange={onToggle}
        disabled={disabled}
        onClick={(e) => e.stopPropagation()}
      />
      <div className="contact-avatar">
        <div className="avatar-placeholder">{initial}</div>
      </div>
      <div className="contact-info">
        <span className="contact-name">{displayName}</span>
        {contact.username && <span className="contact-username">@{contact.username}</span>}
        <span className="contact-phone">{contact.phone}</span>
      </div>
    </div>
  );
}

interface SendResultViewProps {
  result: SendResult;
  onBack: () => void;
  onClose: () => void;
}

function SendResultView({ result, onBack, onClose }: SendResultViewProps) {
  const [activeTab, setActiveTab] = useState<'successful' | 'failed'>('successful');

  const successResults = result.results.filter(r => r.success);
  const failedResults = result.results.filter(r => !r.success);

  return (
    <div className="send-result">
      <div className="result-summary">
        <div className="summary-item success">
          <span className="summary-count">{result.successful}</span>
          <span className="summary-label">Sent</span>
        </div>
        <div className="summary-item error">
          <span className="summary-count">{result.failed}</span>
          <span className="summary-label">Failed</span>
        </div>
      </div>

      <div className="result-tabs">
        <button
          className={`tab ${activeTab === 'successful' ? 'active' : ''}`}
          onClick={() => setActiveTab('successful')}
        >
          Sent ({successResults.length})
        </button>
        <button
          className={`tab ${activeTab === 'failed' ? 'active' : ''}`}
          onClick={() => setActiveTab('failed')}
        >
          Failed ({failedResults.length})
        </button>
      </div>

      <div className="result-content">
        {activeTab === 'successful' && (
          <div className="results-list">
            {successResults.length === 0 ? (
              <p className="empty-message">No messages sent successfully</p>
            ) : (
              successResults.map((r) => (
                <ResultItem key={r.contact_id} result={r} />
              ))
            )}
          </div>
        )}

        {activeTab === 'failed' && (
          <div className="results-list">
            {failedResults.length === 0 ? (
              <p className="empty-message">No failed messages</p>
            ) : (
              failedResults.map((r) => (
                <ResultItem key={r.contact_id} result={r} />
              ))
            )}
          </div>
        )}
      </div>

      <div className="modal-actions">
        <button className="btn-secondary" onClick={onBack}>
          Send More
        </button>
        <button className="btn-primary" onClick={onClose}>
          Done
        </button>
      </div>
    </div>
  );
}

function ResultItem({ result }: { result: RecipientResult }) {
  const initial = (result.name || result.phone || 'U').charAt(0).toUpperCase();

  return (
    <div className={`result-item ${result.success ? 'success' : 'failed'}`}>
      <div className="result-avatar">
        <div className="avatar-placeholder">{initial}</div>
      </div>
      <div className="result-info">
        <span className="result-name">{result.name}</span>
        <span className="result-phone">{result.phone}</span>
        {result.error && <span className="result-error">{result.error}</span>}
      </div>
      <div className="result-status">
        {result.success ? (
          <span className="status-icon success">&#10003;</span>
        ) : (
          <span className="status-icon failed">&#10007;</span>
        )}
      </div>
    </div>
  );
}

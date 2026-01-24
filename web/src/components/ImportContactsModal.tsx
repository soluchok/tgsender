import { useState, useMemo, useRef } from 'react';
import { TelegramAccount } from '../types';

const API_URL = import.meta.env.VITE_API_URL || '';

interface ImportContactsModalProps {
  account: TelegramAccount;
  onClose: () => void;
  onImported: () => void;
}

interface ImportContact {
  telegram_id: number;
  phone: string;
  first_name: string;
  last_name?: string;
  username?: string;
  photo_url?: string;
  labels?: string[];
}

interface ImportResult {
  imported: number;
  skipped: number;
  failed: number;
  errors: string[];
}

export function ImportContactsModal({ account, onClose, onImported }: ImportContactsModalProps) {
  const [parsedContacts, setParsedContacts] = useState<ImportContact[]>([]);
  const [selectedContactIds, setSelectedContactIds] = useState<Set<number>>(new Set());
  const [searchQuery, setSearchQuery] = useState('');
  const [isImporting, setIsImporting] = useState(false);
  const [parseError, setParseError] = useState<string | null>(null);
  const [importResult, setImportResult] = useState<ImportResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  // Filter contacts based on search query
  const filteredContacts = useMemo(() => {
    if (!searchQuery.trim()) {
      return parsedContacts;
    }
    
    const query = searchQuery.toLowerCase().trim().replace(/^@/, '');
    return parsedContacts.filter(contact => {
      const fullName = [contact.first_name, contact.last_name].filter(Boolean).join(' ').toLowerCase() || 'unknown';
      return fullName.includes(query) ||
        (contact.username && contact.username.toLowerCase().includes(query)) ||
        (contact.phone && contact.phone.includes(query));
    });
  }, [parsedContacts, searchQuery]);

  const handleFileSelect = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    setParseError(null);
    setError(null);
    setImportResult(null);

    try {
      const text = await file.text();
      const data = JSON.parse(text);

      // Validate the data structure
      if (!Array.isArray(data)) {
        setParseError('Invalid file format: expected an array of contacts');
        return;
      }

      // Map to import contact format and validate
      const contacts: ImportContact[] = [];
      for (let i = 0; i < data.length; i++) {
        const item = data[i];
        
        // Must have telegram_id
        if (!item.telegram_id || typeof item.telegram_id !== 'number') {
          setParseError(`Invalid contact at index ${i}: missing or invalid telegram_id`);
          return;
        }

        // Must have phone or username for resolution
        if (!item.phone && !item.username) {
          setParseError(`Contact at index ${i} (${item.first_name || 'Unknown'}) has no phone or username - cannot resolve`);
          return;
        }

        contacts.push({
          telegram_id: item.telegram_id,
          phone: item.phone || '',
          first_name: item.first_name || '',
          last_name: item.last_name || '',
          username: item.username || '',
          photo_url: item.photo_url || '',
          labels: item.labels || [],
        });
      }

      if (contacts.length === 0) {
        setParseError('No valid contacts found in the file');
        return;
      }

      setParsedContacts(contacts);
      setSelectedContactIds(new Set(contacts.map(c => c.telegram_id)));
    } catch (err) {
      setParseError(err instanceof Error ? err.message : 'Failed to parse file');
    }

    // Reset file input so same file can be selected again
    if (fileInputRef.current) {
      fileInputRef.current.value = '';
    }
  };

  const handleToggleContact = (telegramId: number) => {
    setSelectedContactIds(prev => {
      const next = new Set(prev);
      if (next.has(telegramId)) {
        next.delete(telegramId);
      } else {
        next.add(telegramId);
      }
      return next;
    });
  };

  const handleSelectAll = () => {
    const filteredIds = new Set(filteredContacts.map(c => c.telegram_id));
    const allFilteredSelected = filteredContacts.every(c => selectedContactIds.has(c.telegram_id));
    
    if (allFilteredSelected) {
      setSelectedContactIds(prev => {
        const next = new Set(prev);
        filteredIds.forEach(id => next.delete(id));
        return next;
      });
    } else {
      setSelectedContactIds(prev => {
        const next = new Set(prev);
        filteredIds.forEach(id => next.add(id));
        return next;
      });
    }
  };

  const handleImport = async () => {
    const contactsToImport = parsedContacts.filter(c => selectedContactIds.has(c.telegram_id));
    
    if (contactsToImport.length === 0) {
      setError('Please select at least one contact');
      return;
    }

    setError(null);
    setIsImporting(true);

    try {
      const response = await fetch(`${API_URL}/api/accounts/${account.id}/import-file`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        credentials: 'include',
        body: JSON.stringify({ contacts: contactsToImport }),
      });

      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || 'Failed to import contacts');
      }

      setImportResult(data);
      
      // If there were any successful imports, notify parent
      if (data.imported > 0 || data.skipped > 0) {
        onImported();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to import contacts');
    } finally {
      setIsImporting(false);
    }
  };

  const handleReset = () => {
    setParsedContacts([]);
    setSelectedContactIds(new Set());
    setSearchQuery('');
    setParseError(null);
    setImportResult(null);
    setError(null);
  };

  const allFilteredSelected = filteredContacts.length > 0 && 
    filteredContacts.every(c => selectedContactIds.has(c.telegram_id));

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal import-contacts-modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <h2>Import Contacts</h2>
          <button className="modal-close" onClick={onClose}>
            &times;
          </button>
        </div>

        <div className="modal-content">
          {importResult ? (
            // Show import results
            <div className="import-result">
              <h3>Import Complete</h3>
              <div className="import-stats">
                <div className="stat">
                  <span className="stat-value success">{importResult.imported}</span>
                  <span className="stat-label">Imported</span>
                </div>
                <div className="stat">
                  <span className="stat-value warning">{importResult.skipped}</span>
                  <span className="stat-label">Skipped (already exist)</span>
                </div>
                <div className="stat">
                  <span className="stat-value error">{importResult.failed}</span>
                  <span className="stat-label">Failed</span>
                </div>
              </div>
              
              {importResult.errors.length > 0 && (
                <div className="import-errors">
                  <h4>Errors:</h4>
                  <ul className="error-list">
                    {importResult.errors.slice(0, 10).map((err, i) => (
                      <li key={i}>{err}</li>
                    ))}
                    {importResult.errors.length > 10 && (
                      <li>... and {importResult.errors.length - 10} more</li>
                    )}
                  </ul>
                </div>
              )}

              <div className="modal-actions">
                <button className="btn-secondary" onClick={handleReset}>
                  Import More
                </button>
                <button className="btn-primary" onClick={onClose}>
                  Done
                </button>
              </div>
            </div>
          ) : parsedContacts.length === 0 ? (
            // File upload section
            <div className="file-upload-section">
              <p className="upload-description">
                Select a JSON file exported from another account. Contacts will be resolved
                to ensure they work with this account.
              </p>
              
              <div className="file-upload-area">
                <input
                  ref={fileInputRef}
                  type="file"
                  accept=".json,application/json"
                  onChange={handleFileSelect}
                  id="import-file-input"
                  className="file-input"
                />
                <label htmlFor="import-file-input" className="file-upload-label">
                  <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
                    <polyline points="17 8 12 3 7 8"></polyline>
                    <line x1="12" y1="3" x2="12" y2="15"></line>
                  </svg>
                  <span>Click to select a file</span>
                  <span className="file-hint">or drag and drop a JSON file here</span>
                </label>
              </div>

              {parseError && (
                <div className="error-message">{parseError}</div>
              )}
            </div>
          ) : (
            // Contact selection section
            <>
              <div className="contacts-selection">
                <div className="selection-header">
                  <label>
                    <input
                      type="checkbox"
                      checked={allFilteredSelected}
                      onChange={handleSelectAll}
                      disabled={isImporting}
                    />
                    <span>Select All ({filteredContacts.length} contacts)</span>
                  </label>
                  <input
                    type="text"
                    className="contact-search-input"
                    placeholder="Search contacts..."
                    value={searchQuery}
                    onChange={(e) => setSearchQuery(e.target.value)}
                    disabled={isImporting}
                  />
                  <span className="selected-count">
                    {selectedContactIds.size} selected
                  </span>
                </div>

                <div className="contacts-list selectable">
                  {filteredContacts.length === 0 ? (
                    <div className="empty-message">No contacts found</div>
                  ) : (
                    filteredContacts.map((contact) => (
                      <ContactSelectItem
                        key={contact.telegram_id}
                        contact={contact}
                        selected={selectedContactIds.has(contact.telegram_id)}
                        onToggle={() => handleToggleContact(contact.telegram_id)}
                        disabled={isImporting}
                      />
                    ))
                  )}
                </div>
              </div>

              {error && (
                <div className="error-message">{error}</div>
              )}

              <div className="modal-actions">
                <button
                  className="btn-secondary"
                  onClick={handleReset}
                  disabled={isImporting}
                >
                  Change File
                </button>
                <button
                  className="btn-primary"
                  onClick={handleImport}
                  disabled={isImporting || selectedContactIds.size === 0}
                >
                  {isImporting ? (
                    <>
                      <div className="loading-spinner small" />
                      <span>Importing...</span>
                    </>
                  ) : (
                    `Import ${selectedContactIds.size} Contacts`
                  )}
                </button>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

// Contact Select Item Component
interface ContactSelectItemProps {
  contact: ImportContact;
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
        {contact.photo_url ? (
          <img src={contact.photo_url} alt={displayName} className="avatar-image" />
        ) : (
          <div className="avatar-placeholder">{initial}</div>
        )}
      </div>
      <div className="contact-info">
        <div className="contact-name-row">
          <span className="contact-name">{displayName}</span>
          {contact.labels && contact.labels.length > 0 && (
            <div className="contact-labels">
              {contact.labels.map((label, index) => (
                <span key={index} className="contact-label-small">
                  {label}
                </span>
              ))}
            </div>
          )}
        </div>
        {contact.username && <span className="contact-username">@{contact.username}</span>}
        {contact.phone && <span className="contact-phone">{contact.phone}</span>}
      </div>
    </div>
  );
}

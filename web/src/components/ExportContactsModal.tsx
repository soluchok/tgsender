import { useState, useMemo } from 'react';
import { Contact } from '../types';

interface ExportContactsModalProps {
  contacts: Contact[];
  onClose: () => void;
}

export function ExportContactsModal({ contacts, onClose }: ExportContactsModalProps) {
  const [selectedContactIds, setSelectedContactIds] = useState<Set<string>>(
    new Set(contacts.map(c => c.id))
  );
  const [selectedLabels, setSelectedLabels] = useState<Set<string>>(new Set());
  const [searchQuery, setSearchQuery] = useState('');
  const [isExporting, setIsExporting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Get unique labels from contacts
  const availableLabels = useMemo(() => {
    const labels = new Set<string>();
    contacts.forEach(contact => {
      contact.labels?.forEach(label => labels.add(label));
    });
    return Array.from(labels).sort();
  }, [contacts]);

  // Filter contacts based on search query and selected labels
  const filteredContacts = useMemo(() => {
    let filtered = contacts;
    
    if (selectedLabels.size > 0) {
      filtered = filtered.filter(contact =>
        contact.labels?.some(label => selectedLabels.has(label))
      );
    }
    
    if (searchQuery.trim()) {
      const query = searchQuery.toLowerCase().trim().replace(/^@/, '');
      filtered = filtered.filter(contact => {
        const fullName = [contact.first_name, contact.last_name].filter(Boolean).join(' ').toLowerCase() || 'unknown';
        return fullName.includes(query) ||
          (contact.username && contact.username.toLowerCase().includes(query)) ||
          (contact.phone && contact.phone.includes(query));
      });
    }
    
    return filtered;
  }, [contacts, selectedLabels, searchQuery]);

  const handleToggleContact = (contactId: string) => {
    setSelectedContactIds(prev => {
      const next = new Set(prev);
      if (next.has(contactId)) {
        next.delete(contactId);
      } else {
        next.add(contactId);
      }
      return next;
    });
  };

  const handleSelectAll = () => {
    const filteredIds = new Set(filteredContacts.map(c => c.id));
    const allFilteredSelected = filteredContacts.every(c => selectedContactIds.has(c.id));
    
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

  const handleToggleLabel = (label: string) => {
    setSelectedLabels(prev => {
      const next = new Set(prev);
      if (next.has(label)) {
        next.delete(label);
      } else {
        next.add(label);
      }
      return next;
    });
  };

  const handleClearLabelFilter = () => {
    setSelectedLabels(new Set());
  };

  const handleExport = async () => {
    const contactsToExport = contacts.filter(c => selectedContactIds.has(c.id));
    
    if (contactsToExport.length === 0) {
      setError('Please select at least one contact');
      return;
    }

    setError(null);
    setIsExporting(true);

    try {
      const blob = new Blob([JSON.stringify(contactsToExport, null, 2)], { type: 'application/json' });
      const url = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `contacts-${new Date().toISOString().split('T')[0]}.json`;
      document.body.appendChild(a);
      a.click();
      window.URL.revokeObjectURL(url);
      document.body.removeChild(a);

      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to export contacts');
    } finally {
      setIsExporting(false);
    }
  };

  const allFilteredSelected = filteredContacts.length > 0 && 
    filteredContacts.every(c => selectedContactIds.has(c.id));

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal export-contacts-modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <h2>Export Contacts</h2>
          <button className="modal-close" onClick={onClose}>
            &times;
          </button>
        </div>

        <div className="modal-content">
          {availableLabels.length > 0 && (
            <div className="form-group">
              <label>Filter by Labels</label>
              <div className="label-filter">
                {availableLabels.map(label => (
                  <button
                    key={label}
                    className={`label-filter-btn ${selectedLabels.has(label) ? 'active' : ''}`}
                    onClick={() => handleToggleLabel(label)}
                    disabled={isExporting}
                  >
                    {label}
                  </button>
                ))}
                {selectedLabels.size > 0 && (
                  <button
                    className="label-filter-clear"
                    onClick={handleClearLabelFilter}
                    disabled={isExporting}
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
                  checked={allFilteredSelected}
                  onChange={handleSelectAll}
                  disabled={isExporting}
                />
                <span>Select All ({filteredContacts.length} contacts)</span>
              </label>
              <input
                type="text"
                className="contact-search-input"
                placeholder="Search contacts..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                disabled={isExporting}
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
                    key={contact.id}
                    contact={contact}
                    selected={selectedContactIds.has(contact.id)}
                    onToggle={() => handleToggleContact(contact.id)}
                    disabled={isExporting}
                  />
                ))
              )}
            </div>
          </div>

          {error && (
            <div className="error-message">{error}</div>
          )}
        </div>

        <div className="modal-actions">
          <button
            className="btn-secondary"
            onClick={onClose}
            disabled={isExporting}
          >
            Cancel
          </button>
          <button
            className="btn-primary"
            onClick={handleExport}
            disabled={isExporting || selectedContactIds.size === 0}
          >
            {isExporting ? (
              <>
                <div className="loading-spinner small" />
                <span>Exporting...</span>
              </>
            ) : (
              'Export'
            )}
          </button>
        </div>
      </div>
    </div>
  );
}

// Contact Select Item Component
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
        <span className="contact-phone">{contact.phone}</span>
      </div>
    </div>
  );
}

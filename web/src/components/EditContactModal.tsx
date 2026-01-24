import { useState } from 'react';
import { Contact } from '../types';

const API_URL = import.meta.env.VITE_API_URL || '';

interface EditContactModalProps {
  contact: Contact;
  onClose: () => void;
  onSave: (updatedContact: Contact) => void;
}

export function EditContactModal({ contact, onClose, onSave }: EditContactModalProps) {
  const [firstName, setFirstName] = useState(contact.first_name || '');
  const [lastName, setLastName] = useState(contact.last_name || '');
  const [labelsInput, setLabelsInput] = useState((contact.labels || []).join(', '));
  const [isSaving, setIsSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setIsSaving(true);

    // Parse labels from comma-separated input
    const labels = labelsInput
      .split(',')
      .map(l => l.trim().toLowerCase())
      .filter(l => l.length > 0);

    try {
      const response = await fetch(`${API_URL}/api/contacts/${contact.id}/update`, {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
        },
        credentials: 'include',
        body: JSON.stringify({
          first_name: firstName,
          last_name: lastName,
          labels,
        }),
      });

      if (!response.ok) {
        const data = await response.json();
        throw new Error(data.error || 'Failed to update contact');
      }

      const updatedContact = await response.json();
      onSave(updatedContact);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update contact');
    } finally {
      setIsSaving(false);
    }
  };

  const displayName = [contact.first_name, contact.last_name].filter(Boolean).join(' ') || 'Unknown';
  const initial = (contact.first_name || contact.phone || 'U').charAt(0).toUpperCase();

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal edit-contact-modal" onClick={e => e.stopPropagation()}>
        <div className="modal-header">
          <h2>Edit Contact</h2>
          <button className="modal-close" onClick={onClose}>&times;</button>
        </div>

        <div className="modal-content">
          <div className="edit-contact-header">
            <div className="edit-contact-avatar">
              {contact.photo_url ? (
                <img src={contact.photo_url} alt={displayName} className="avatar-image" />
              ) : (
                <div className="avatar-placeholder">
                  {initial}
                </div>
              )}
            </div>
            <div className="edit-contact-info">
              {contact.username && (
                <span className="edit-contact-username">@{contact.username}</span>
              )}
              {contact.phone && (
                <span className="edit-contact-phone">{contact.phone}</span>
              )}
            </div>
          </div>

          {error && (
            <div className="error-message">{error}</div>
          )}

          <form onSubmit={handleSubmit}>
            <div className="form-group">
              <label htmlFor="firstName">First Name</label>
              <input
                type="text"
                id="firstName"
                className="labels-input"
                value={firstName}
                onChange={(e) => setFirstName(e.target.value)}
                disabled={isSaving}
                placeholder="Enter first name"
              />
            </div>

            <div className="form-group">
              <label htmlFor="lastName">Last Name</label>
              <input
                type="text"
                id="lastName"
                className="labels-input"
                value={lastName}
                onChange={(e) => setLastName(e.target.value)}
                disabled={isSaving}
                placeholder="Enter last name"
              />
            </div>

            <div className="form-group">
              <label htmlFor="labels">Labels</label>
              <input
                type="text"
                id="labels"
                className="labels-input"
                value={labelsInput}
                onChange={(e) => setLabelsInput(e.target.value)}
                disabled={isSaving}
                placeholder="e.g., friend, work, vip"
              />
              <span className="input-hint">Separate multiple labels with commas</span>
            </div>

            <div className="modal-actions">
              <button
                type="button"
                className="btn-secondary"
                onClick={onClose}
                disabled={isSaving}
              >
                Cancel
              </button>
              <button
                type="submit"
                className="btn-primary"
                disabled={isSaving}
              >
                {isSaving ? (
                  <>
                    <div className="loading-spinner small" />
                    Saving...
                  </>
                ) : (
                  'Save Changes'
                )}
              </button>
            </div>
          </form>
        </div>
      </div>
    </div>
  );
}

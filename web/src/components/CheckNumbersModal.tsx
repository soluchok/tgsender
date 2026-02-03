import { useState } from 'react';
import { TelegramAccount, CheckNumbersResult, Contact } from '../types';
import { apiFetch, isUnauthorizedError } from '../utils/api';

interface CheckNumbersModalProps {
  account: TelegramAccount;
  onClose: () => void;
}

export function CheckNumbersModal({ account, onClose }: CheckNumbersModalProps) {
  const [inputs, setInputs] = useState('');
  const [labels, setLabels] = useState('');
  const [isChecking, setIsChecking] = useState(false);
  const [result, setResult] = useState<CheckNumbersResult | null>(null);
  const [error, setError] = useState<string | null>(null);

  const handleCheck = async () => {
    setError(null);
    setResult(null);

    // Parse inputs (one per line, or comma-separated)
    const inputList = inputs
      .split(/[\n,]/)
      .map(p => p.trim())
      .filter(p => p.length > 0);

    if (inputList.length === 0) {
      setError('Please enter at least one phone number or username');
      return;
    }

    // Parse labels (comma-separated)
    const labelList = labels
      .split(',')
      .map(l => l.trim())
      .filter(l => l.length > 0);

    setIsChecking(true);

    try {
      const response = await apiFetch(`/api/accounts/${account.id}/check-numbers`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ inputs: inputList, labels: labelList }),
      });

      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || 'Failed to check contacts');
      }

      setResult(data);
    } catch (err) {
      if (isUnauthorizedError(err)) return;
      setError(err instanceof Error ? err.message : 'Failed to check contacts');
    } finally {
      setIsChecking(false);
    }
  };

  const handleClose = () => {
    onClose();
  };

  return (
    <div className="modal-overlay" onClick={handleClose}>
      <div className="modal check-numbers-modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <h2>Add Contacts</h2>
          <button className="modal-close" onClick={handleClose}>
            &times;
          </button>
        </div>

        <div className="modal-content">
          <p className="modal-description">
            Check if phone numbers or usernames are registered on Telegram using account: <strong>{account.first_name}</strong>
          </p>

          {!result ? (
            <>
              <div className="form-group">
                <label htmlFor="inputs">Phone Numbers or Usernames</label>
                <textarea
                  id="inputs"
                  value={inputs}
                  onChange={(e) => setInputs(e.target.value)}
                  placeholder="Enter phone numbers or usernames (one per line or comma-separated)&#10;e.g.:&#10;+1234567890&#10;@username&#10;johndoe"
                  rows={8}
                  disabled={isChecking}
                  className="phone-input"
                />
                <span className="input-hint">
                  Phone numbers should include country code (e.g., +1 for US). Usernames can start with @ or without.
                </span>
              </div>

              <div className="form-group">
                <label htmlFor="labels">Labels (optional)</label>
                <input
                  type="text"
                  id="labels"
                  value={labels}
                  onChange={(e) => setLabels(e.target.value)}
                  placeholder="e.g., customer, lead, vip"
                  disabled={isChecking}
                  className="labels-input"
                />
                <span className="input-hint">
                  Comma-separated labels to tag these contacts. Leave empty to auto-assign based on input type.
                </span>
              </div>

              {error && (
                <div className="error-message">{error}</div>
              )}

              <div className="modal-actions">
                <button
                  className="btn-secondary"
                  onClick={handleClose}
                  disabled={isChecking}
                >
                  Cancel
                </button>
                <button
                  className="btn-primary"
                  onClick={handleCheck}
                  disabled={isChecking || !inputs.trim()}
                >
                  {isChecking ? (
                    <>
                      <div className="loading-spinner small" />
                      <span>Checking...</span>
                    </>
                  ) : (
                    'Check Contacts'
                  )}
                </button>
              </div>
            </>
          ) : (
            <CheckResultView result={result} onBack={() => setResult(null)} onClose={handleClose} />
          )}
        </div>
      </div>
    </div>
  );
}

interface CheckResultViewProps {
  result: CheckNumbersResult;
  onBack: () => void;
  onClose: () => void;
}

function CheckResultView({ result, onBack, onClose }: CheckResultViewProps) {
  const [activeTab, setActiveTab] = useState<'valid' | 'invalid' | 'retry'>('valid');

  return (
    <div className="check-result">
      <div className="result-summary">
        <div className="summary-item success">
          <span className="summary-count">{result.valid_count}</span>
          <span className="summary-label">Valid</span>
        </div>
        <div className="summary-item error">
          <span className="summary-count">{result.invalid.length}</span>
          <span className="summary-label">Invalid</span>
        </div>
        <div className="summary-item warning">
          <span className="summary-count">{result.retry.length}</span>
          <span className="summary-label">Retry</span>
        </div>
      </div>

      {result.errors.length > 0 && (
        <div className="result-errors">
          {result.errors.map((err, i) => (
            <div key={i} className="error-message">{err}</div>
          ))}
        </div>
      )}

      <div className="result-tabs">
        <button
          className={`tab ${activeTab === 'valid' ? 'active' : ''}`}
          onClick={() => setActiveTab('valid')}
        >
          Valid ({result.valid.length})
        </button>
        <button
          className={`tab ${activeTab === 'invalid' ? 'active' : ''}`}
          onClick={() => setActiveTab('invalid')}
        >
          Invalid ({result.invalid.length})
        </button>
        <button
          className={`tab ${activeTab === 'retry' ? 'active' : ''}`}
          onClick={() => setActiveTab('retry')}
        >
          Retry ({result.retry.length})
        </button>
      </div>

      <div className="result-content">
        {activeTab === 'valid' && (
          <div className="contacts-list">
            {result.valid.length === 0 ? (
              <p className="empty-message">No valid contacts found</p>
            ) : (
              result.valid.map((contact) => (
                <ContactItem key={contact.id} contact={contact} />
              ))
            )}
          </div>
        )}

        {activeTab === 'invalid' && (
          <div className="phones-list">
            {result.invalid.length === 0 ? (
              <p className="empty-message">No invalid numbers</p>
            ) : (
              result.invalid.map((phone, i) => (
                <div key={i} className="phone-item invalid">
                  {phone}
                </div>
              ))
            )}
          </div>
        )}

        {activeTab === 'retry' && (
          <div className="phones-list">
            {result.retry.length === 0 ? (
              <p className="empty-message">No numbers to retry</p>
            ) : (
              result.retry.map((phone, i) => (
                <div key={i} className="phone-item retry">
                  {phone}
                </div>
              ))
            )}
          </div>
        )}
      </div>

      <div className="modal-actions">
        <button className="btn-secondary" onClick={onBack}>
          Check More
        </button>
        <button className="btn-primary" onClick={onClose}>
          Done
        </button>
      </div>
    </div>
  );
}

function ContactItem({ contact }: { contact: Contact }) {
  const displayName = [contact.first_name, contact.last_name].filter(Boolean).join(' ') || 'Unknown';

  return (
    <div className="contact-item">
      <div className="contact-avatar">
        <div className="avatar-placeholder">
          {(contact.first_name || 'U').charAt(0).toUpperCase()}
        </div>
      </div>
      <div className="contact-info">
        <span className="contact-name">{displayName}</span>
        {contact.username && <span className="contact-username">@{contact.username}</span>}
        <span className="contact-phone">{contact.phone}</span>
      </div>
    </div>
  );
}

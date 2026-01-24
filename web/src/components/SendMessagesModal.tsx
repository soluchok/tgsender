import { useState, useMemo, useEffect, useRef, useCallback } from 'react';
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

interface SendJob {
  id: string;
  account_id: string;
  status: 'pending' | 'running' | 'completed' | 'failed';
  message: string;
  total: number;
  sent: number;
  failed: number;
  results: RecipientResult[];
  error?: string;
  started_at: string;
  updated_at: string;
}

type ViewMode = 'compose' | 'progress' | 'result' | 'history';

export function SendMessagesModal({ account, contacts, onClose }: SendMessagesModalProps) {
  const [viewMode, setViewMode] = useState<ViewMode>('compose');
  const [message, setMessage] = useState('');
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set(contacts.map(c => c.id)));
  const [isSending, setIsSending] = useState(false);
  const [currentJob, setCurrentJob] = useState<SendJob | null>(null);
  const [jobHistory, setJobHistory] = useState<SendJob[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [selectedLabels, setSelectedLabels] = useState<Set<string>>(new Set());
  const [delayMin, setDelayMin] = useState(0);
  const [delayMax, setDelayMax] = useState(60);
  const [searchQuery, setSearchQuery] = useState('');
  const [hasOpenAIToken, setHasOpenAIToken] = useState(false);
  const [aiPrompt, setAiPrompt] = useState('');
  const [useAI, setUseAI] = useState(false);
  const pollIntervalRef = useRef<number | null>(null);

  // Check if account has OpenAI token
  useEffect(() => {
    const checkSettings = async () => {
      try {
        const response = await fetch(`${API_URL}/api/accounts/${account.id}/settings`, {
          credentials: 'include',
        });
        if (response.ok) {
          const data = await response.json();
          setHasOpenAIToken(data.has_openai_token || false);
        }
      } catch (err) {
        console.error('Failed to check account settings:', err);
      }
    };
    checkSettings();
  }, [account.id]);

  // Cleanup polling on unmount
  useEffect(() => {
    return () => {
      if (pollIntervalRef.current) {
        clearInterval(pollIntervalRef.current);
      }
    };
  }, []);

  // Fetch job history on mount and when viewing history
  const fetchHistory = useCallback(async () => {
    try {
      const response = await fetch(`${API_URL}/api/accounts/${account.id}/send/history`, {
        credentials: 'include',
      });
      if (response.ok) {
        const data = await response.json();
        setJobHistory(data.jobs || []);
      }
    } catch (err) {
      console.error('Failed to fetch history:', err);
    }
  }, [account.id]);

  useEffect(() => {
    fetchHistory();
  }, [fetchHistory]);

  // Poll for job status
  const pollJobStatus = useCallback(async (jobId: string) => {
    try {
      const response = await fetch(
        `${API_URL}/api/accounts/${account.id}/send/status?job_id=${jobId}`,
        { credentials: 'include' }
      );
      if (response.ok) {
        const job: SendJob = await response.json();
        setCurrentJob(job);

        if (job.status === 'completed' || job.status === 'failed') {
          if (pollIntervalRef.current) {
            clearInterval(pollIntervalRef.current);
            pollIntervalRef.current = null;
          }
          setViewMode('result');
          setIsSending(false);
          fetchHistory(); // Refresh history
        }
      }
    } catch (err) {
      console.error('Failed to poll job status:', err);
    }
  }, [account.id, fetchHistory]);

  const startPolling = useCallback((jobId: string) => {
    if (pollIntervalRef.current) {
      clearInterval(pollIntervalRef.current);
    }
    pollIntervalRef.current = window.setInterval(() => pollJobStatus(jobId), 1000);
  }, [pollJobStatus]);

  // Format delay for display
  const formatDelay = (min: number, max: number) => {
    if (min === 0 && max === 0) return 'No delay';
    if (min === max) return `${min}s`;
    return `${min}s - ${max}s`;
  };

  // Handle min delay change
  const handleDelayMinChange = (value: number) => {
    setDelayMin(value);
    if (value > delayMax) {
      setDelayMax(value);
    }
  };

  // Handle max delay change
  const handleDelayMaxChange = (value: number) => {
    setDelayMax(value);
    if (value < delayMin) {
      setDelayMin(value);
    }
  };

  // Extract unique labels from all contacts
  const availableLabels = useMemo(() => {
    const labels = new Set<string>();
    contacts.forEach(contact => {
      contact.labels?.forEach(label => labels.add(label));
    });
    return Array.from(labels).sort();
  }, [contacts]);

  // Filter contacts based on selected labels and search query
  const filteredContacts = useMemo(() => {
    let filtered = contacts;
    
    if (selectedLabels.size > 0) {
      filtered = filtered.filter(contact =>
        contact.labels?.some(label => selectedLabels.has(label))
      );
    }
    
    if (searchQuery.trim()) {
      const query = searchQuery.toLowerCase().trim();
      filtered = filtered.filter(contact => {
        const firstName = (contact.first_name || '').toLowerCase();
        const lastName = (contact.last_name || '').toLowerCase();
        const username = (contact.username || '').toLowerCase();
        const phone = (contact.phone || '').toLowerCase();
        return firstName.includes(query) || 
               lastName.includes(query) || 
               username.includes(query) || 
               phone.includes(query);
      });
    }
    
    return filtered;
  }, [contacts, selectedLabels, searchQuery]);

  const handleToggleLabel = (label: string) => {
    const newLabels = new Set(selectedLabels);
    if (newLabels.has(label)) {
      newLabels.delete(label);
    } else {
      newLabels.add(label);
    }
    setSelectedLabels(newLabels);

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

    if (selectedIds.size === 0) {
      setError('Please select at least one contact');
      return;
    }

    if (!message.trim()) {
      setError('Please enter a message');
      return;
    }

    if (useAI && !aiPrompt.trim()) {
      setError('Please enter AI instructions or disable AI rewriting');
      return;
    }

    setIsSending(true);
    setViewMode('progress');

    try {
      const requestBody: Record<string, unknown> = {
        contact_ids: Array.from(selectedIds),
        message: message.trim(),
        delay_min_ms: delayMin * 1000,
        delay_max_ms: delayMax * 1000,
      };

      if (useAI && aiPrompt.trim()) {
        requestBody.ai_prompt = aiPrompt.trim();
      }

      const response = await fetch(`${API_URL}/api/accounts/${account.id}/send`, {
        method: 'POST',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(requestBody),
      });

      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || 'Failed to send messages');
      }

      // Set initial job state and start polling
      setCurrentJob({
        ...data,
        message: message.trim(),
        results: [],
      });
      startPolling(data.id);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to send messages');
      setViewMode('compose');
      setIsSending(false);
    }
  };

  const handleRetry = async (jobId: string) => {
    setError(null);
    setIsSending(true);
    setViewMode('progress');

    try {
      const response = await fetch(`${API_URL}/api/accounts/${account.id}/send/retry`, {
        method: 'POST',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ job_id: jobId }),
      });

      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || 'Failed to retry');
      }

      setCurrentJob({
        ...data,
        message: currentJob?.message || '',
        results: [],
      });
      startPolling(data.id);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to retry');
      setViewMode('result');
      setIsSending(false);
    }
  };

  const handleViewHistory = () => {
    fetchHistory();
    setViewMode('history');
  };

  const handleViewJob = (job: SendJob) => {
    setCurrentJob(job);
    setViewMode('result');
  };

  const handleBackToCompose = () => {
    setCurrentJob(null);
    setViewMode('compose');
  };

  const handleClose = () => {
    if (pollIntervalRef.current) {
      clearInterval(pollIntervalRef.current);
    }
    onClose();
  };

  const allSelected = selectedIds.size === filteredContacts.length && filteredContacts.length > 0;
  const noneSelected = selectedIds.size === 0;

  return (
    <div className="modal-overlay" onClick={handleClose}>
      <div className="modal send-messages-modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <div>
            <h2 style={{ display: "inline" }}>Send Messages</h2>
            <span> ({account.first_name} {account.last_name}) </span>
          </div>
          <div className="modal-header-actions">
            {viewMode === 'compose' && jobHistory.length > 0 && (
              <button className="btn-text" onClick={handleViewHistory}>
                History ({jobHistory.length})
              </button>
            )}
            <button className="modal-close" onClick={handleClose}>
              &times;
            </button>
          </div>
        </div>

        <div className="modal-content">
          {viewMode === 'compose' && (
            <ComposeView
              contacts={contacts}
              filteredContacts={filteredContacts}
              selectedIds={selectedIds}
              selectedLabels={selectedLabels}
              availableLabels={availableLabels}
              message={message}
              delayMin={delayMin}
              delayMax={delayMax}
              searchQuery={searchQuery}
              isSending={isSending}
              error={error}
              allSelected={allSelected}
              hasOpenAIToken={hasOpenAIToken}
              useAI={useAI}
              aiPrompt={aiPrompt}
              formatDelay={formatDelay}
              onMessageChange={setMessage}
              onDelayMinChange={handleDelayMinChange}
              onDelayMaxChange={handleDelayMaxChange}
              onSearchChange={setSearchQuery}
              onToggleLabel={handleToggleLabel}
              onClearLabelFilter={handleClearLabelFilter}
              onSelectAll={handleSelectAll}
              onToggleContact={handleToggleContact}
              onUseAIChange={setUseAI}
              onAiPromptChange={setAiPrompt}
            />
          )}

          {viewMode === 'progress' && currentJob && (
            <ProgressView job={currentJob} />
          )}

          {viewMode === 'result' && currentJob && (
            <ResultView
              job={currentJob}
              onRetry={() => handleRetry(currentJob.id)}
              onBack={handleBackToCompose}
              onClose={handleClose}
            />
          )}

          {viewMode === 'history' && (
            <HistoryView
              jobs={jobHistory}
              onViewJob={handleViewJob}
              onBack={handleBackToCompose}
            />
          )}
        </div>

        {viewMode === 'compose' && (
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
        )}
      </div>
    </div>
  );
}

// Compose View Component
interface ComposeViewProps {
  contacts: Contact[];
  filteredContacts: Contact[];
  selectedIds: Set<string>;
  selectedLabels: Set<string>;
  availableLabels: string[];
  message: string;
  delayMin: number;
  delayMax: number;
  searchQuery: string;
  isSending: boolean;
  error: string | null;
  allSelected: boolean;
  hasOpenAIToken: boolean;
  useAI: boolean;
  aiPrompt: string;
  formatDelay: (min: number, max: number) => string;
  onMessageChange: (msg: string) => void;
  onDelayMinChange: (val: number) => void;
  onDelayMaxChange: (val: number) => void;
  onSearchChange: (query: string) => void;
  onToggleLabel: (label: string) => void;
  onClearLabelFilter: () => void;
  onSelectAll: () => void;
  onToggleContact: (id: string) => void;
  onUseAIChange: (use: boolean) => void;
  onAiPromptChange: (prompt: string) => void;
}

function ComposeView({
  contacts,
  filteredContacts,
  selectedIds,
  selectedLabels,
  availableLabels,
  message,
  delayMin,
  delayMax,
  searchQuery,
  isSending,
  error,
  allSelected,
  hasOpenAIToken,
  useAI,
  aiPrompt,
  formatDelay,
  onMessageChange,
  onDelayMinChange,
  onDelayMaxChange,
  onSearchChange,
  onToggleLabel,
  onClearLabelFilter,
  onSelectAll,
  onToggleContact,
  onUseAIChange,
  onAiPromptChange,
}: ComposeViewProps) {
  if (contacts.length === 0) {
    return (
      <div className="empty-state">
        <p>No contacts available.</p>
        <p className="hint">Use "Check Numbers" to add contacts first.</p>
      </div>
    );
  }

  return (
    <>
      <div className="form-group">
        <label htmlFor="message">Message</label>
        <textarea
          id="message"
          value={message}
          onChange={(e) => onMessageChange(e.target.value)}
          placeholder="Enter your message..."
          rows={4}
          disabled={isSending}
          className="message-input"
        />
        <div className="template-hint">
          <strong>Template variables:</strong> <code>{'{{.FirstName}}'}</code> <code>{'{{.LastName}}'}</code> <code>{'{{.Name}}'}</code> <code>{'{{.Username}}'}</code><br />
          <strong>Random pick:</strong> <code>{'{{pick "Hey" "Hi" "Hello"}}'}</code> - randomly selects one option per message
        </div>
      </div>

      {hasOpenAIToken && (
        <div className="form-group ai-rewrite-section">
          <label className="ai-toggle-label">
            <input
              type="checkbox"
              checked={useAI}
              onChange={(e) => onUseAIChange(e.target.checked)}
              disabled={isSending}
            />
            <span>Use AI to rewrite message for each contact</span>
          </label>
          {useAI && (
            <div className="ai-prompt-wrapper">
              <textarea
                value={aiPrompt}
                onChange={(e) => onAiPromptChange(e.target.value)}
                placeholder="Enter instructions for AI, e.g.: 'Rewrite this message in a friendly, casual tone. Make it feel personal and unique for each recipient.'"
                rows={3}
                disabled={isSending}
                className="ai-prompt-input"
              />
              <div className="ai-prompt-hint">
                The AI will rewrite each personalized message based on these instructions before sending.
              </div>
            </div>
          )}
        </div>
      )}

      <div className="form-group">
        <label>Delay between messages: <strong>{formatDelay(delayMin, delayMax)}</strong></label>
        <div className="delay-range-slider">
          <input
            type="range"
            min="0"
            max="60"
            step="1"
            value={delayMin}
            onChange={(e) => onDelayMinChange(Number(e.target.value))}
            disabled={isSending}
            className="delay-slider delay-slider-min"
          />
          <input
            type="range"
            min="0"
            max="60"
            step="1"
            value={delayMax}
            onChange={(e) => onDelayMaxChange(Number(e.target.value))}
            disabled={isSending}
            className="delay-slider delay-slider-max"
          />
          <div
            className="delay-slider-track"
            style={{
              left: `${(delayMin / 60) * 100}%`,
              width: `${((delayMax - delayMin) / 60) * 100}%`
            }}
          />
        </div>
        <div className="delay-labels">
          <span>0s</span>
          <span>30s</span>
          <span>60s</span>
        </div>
      </div>

      {availableLabels.length > 0 && (
        <div className="form-group">
          <label>Filter by Labels</label>
          <div className="label-filter">
            {availableLabels.map(label => (
              <button
                key={label}
                className={`label-filter-btn ${selectedLabels.has(label) ? 'active' : ''}`}
                onClick={() => onToggleLabel(label)}
                disabled={isSending}
              >
                {label}
              </button>
            ))}
            {selectedLabels.size > 0 && (
              <button
                className="label-filter-clear"
                onClick={onClearLabelFilter}
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
              onChange={onSelectAll}
              disabled={isSending}
            />
            <span>Select All ({filteredContacts.length} contacts)</span>
          </label>
          <input
            type="text"
            className="contact-search-input"
            placeholder="Search contacts..."
            value={searchQuery}
            onChange={(e) => onSearchChange(e.target.value)}
            disabled={isSending}
          />
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
              onToggle={() => onToggleContact(contact.id)}
              disabled={isSending}
            />
          ))}
        </div>
      </div>

      {error && (
        <div className="error-message">{error}</div>
      )}
    </>
  );
}

// Progress View Component
interface ProgressViewProps {
  job: SendJob;
}

function ProgressView({ job }: ProgressViewProps) {
  const progress = job.total > 0 ? ((job.sent + job.failed) / job.total) * 100 : 0;

  return (
    <div className="send-progress">
      <div className="progress-header">
        <h3>Sending Messages...</h3>
        <p className="progress-status">
          {job.status === 'pending' ? 'Starting...' : `Processing ${job.sent + job.failed} of ${job.total}`}
        </p>
      </div>

      <div className="progress-bar-container">
        <div className="progress-bar" style={{ width: `${progress}%` }} />
      </div>

      <div className="progress-stats">
        <div className="stat success">
          <span className="stat-value">{job.sent}</span>
          <span className="stat-label">Sent</span>
        </div>
        <div className="stat error">
          <span className="stat-value">{job.failed}</span>
          <span className="stat-label">Failed</span>
        </div>
        <div className="stat">
          <span className="stat-value">{job.total - job.sent - job.failed}</span>
          <span className="stat-label">Remaining</span>
        </div>
      </div>

      {job.message && (
        <div className="progress-message">
          <label>Message:</label>
          <div className="message-preview">{job.message}</div>
        </div>
      )}
    </div>
  );
}

// Result View Component
interface ResultViewProps {
  job: SendJob;
  onRetry: () => void;
  onBack: () => void;
  onClose: () => void;
}

function ResultView({ job, onRetry, onBack, onClose }: ResultViewProps) {
  const [activeTab, setActiveTab] = useState<'successful' | 'failed'>('successful');

  const successResults = job.results?.filter(r => r.success) || [];
  const failedResults = job.results?.filter(r => !r.success) || [];

  return (
    <div className="send-result">
      <div className="result-summary">
        <div className="summary-item success">
          <span className="summary-count">{job.sent}</span>
          <span className="summary-label">Sent</span>
        </div>
        <div className="summary-item error">
          <span className="summary-count">{job.failed}</span>
          <span className="summary-label">Failed</span>
        </div>
      </div>

      {job.message && (
        <div className="result-message">
          <label>Message sent:</label>
          <div className="message-preview">{job.message}</div>
        </div>
      )}

      {job.error && (
        <div className="error-message">{job.error}</div>
      )}

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
        {job.failed > 0 && (
          <button className="btn-warning" onClick={onRetry}>
            Retry Failed ({job.failed})
          </button>
        )}
        <button className="btn-primary" onClick={onClose}>
          Done
        </button>
      </div>
    </div>
  );
}

// History View Component
interface HistoryViewProps {
  jobs: SendJob[];
  onViewJob: (job: SendJob) => void;
  onBack: () => void;
}

function HistoryView({ jobs, onViewJob, onBack }: HistoryViewProps) {
  const sortedJobs = [...jobs].sort((a, b) => 
    new Date(b.started_at).getTime() - new Date(a.started_at).getTime()
  );

  const formatDate = (dateStr: string) => {
    const date = new Date(dateStr);
    return date.toLocaleString();
  };

  const getStatusBadge = (status: string) => {
    switch (status) {
      case 'completed':
        return <span className="status-badge success">Completed</span>;
      case 'failed':
        return <span className="status-badge error">Failed</span>;
      case 'running':
        return <span className="status-badge running">Running</span>;
      default:
        return <span className="status-badge">{status}</span>;
    }
  };

  return (
    <div className="send-history">
      <div className="history-header">
        <h3>Send History</h3>
      </div>

      {sortedJobs.length === 0 ? (
        <p className="empty-message">No messages sent yet</p>
      ) : (
        <div className="history-list">
          {sortedJobs.map((job) => (
            <div
              key={job.id}
              className="history-item"
              onClick={() => onViewJob(job)}
            >
              <div className="history-item-header">
                <span className="history-date">{formatDate(job.started_at)}</span>
                {getStatusBadge(job.status)}
              </div>
              <div className="history-item-message">
                {job.message ? (job.message.length > 100 ? job.message.substring(0, 100) + '...' : job.message) : 'No message'}
              </div>
              <div className="history-item-stats">
                <span className="stat success">{job.sent} sent</span>
                <span className="stat error">{job.failed} failed</span>
                <span className="stat">of {job.total} total</span>
              </div>
            </div>
          ))}
        </div>
      )}

      <div className="modal-actions">
        <button className="btn-secondary" onClick={onBack}>
          Back
        </button>
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

// Result Item Component
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

import { useAuth } from '../contexts';

export function UserProfile() {
  const { user, logout } = useAuth();

  if (!user) return null;

  const fullName = [user.first_name, user.last_name].filter(Boolean).join(' ');

  return (
    <div className="user-profile">
      <div className="user-info">
        {user.photo_url && (
          <img
            src={user.photo_url}
            alt={fullName}
            className="user-avatar"
          />
        )}
        <div className="user-details">
          <span className="user-name">{fullName}</span>
          {user.username && (
            <span className="user-username">@{user.username}</span>
          )}
        </div>
      </div>
      <button onClick={logout} className="logout-button">
        Logout
      </button>
    </div>
  );
}

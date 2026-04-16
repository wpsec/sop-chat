/**
 * Navbar Component
 * Displays user info and logout button in top right corner
 */
import { useAuth } from '../contexts/AuthContext';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import './Navbar.css';

function Navbar() {
  const { t } = useTranslation();
  const { user, logout } = useAuth();
  const navigate = useNavigate();

  const handleLogout = async () => {
    await logout();
    navigate('/login');
  };

  if (!user) return null;

  // 获取用户名，支持多种可能的字段名
  // 调试：打印用户对象以便排查问题
  if (process.env.NODE_ENV === 'development') {
    console.log('User object:', user);
  }
  
  const username = user.username || user.Username || user.name || 'User';
  // 获取用户名的首字母作为头像
  const avatarLetter = username.charAt(0).toUpperCase();

  return (
    <div className="navbar-card">
      <div className="navbar-profile">
        <div className="navbar-avatar">
          {avatarLetter}
        </div>
        <div className="navbar-details">
          <span className="navbar-label">当前账户</span>
          <span className="navbar-name">{username}</span>
        </div>
      </div>
      <button className="navbar-logout-btn" onClick={handleLogout}>
        {t('nav.logout')}
      </button>
    </div>
  );
}

export default Navbar;

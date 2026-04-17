/**
 * Login Component
 */
import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useAuth } from '../contexts/AuthContext';
import './Login.css';

const LOGIN_MODE_PREFERENCE_KEY = 'preferred_login_mode';

function Login() {
  const { t } = useTranslation();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');
  const [loading, setLoading] = useState(false);
  const [setupStatus, setSetupStatus] = useState(null);
  const [selectedMode, setSelectedMode] = useState('builtin');
  const { login } = useAuth();
  const navigate = useNavigate();

  useEffect(() => {
    const hash = window.location.hash || '';
    const queryIndex = hash.indexOf('?');
    if (queryIndex >= 0) {
      const params = new URLSearchParams(hash.slice(queryIndex + 1));
      const redirectError = params.get('error');
      const redirectMessage = params.get('message');
      if (redirectError) {
        setError(redirectError);
      }
      if (redirectMessage) {
        setMessage(redirectMessage);
      }
    }

    fetch('/api/system/setup-status')
      .then(r => r.ok ? r.json() : null)
      .then(data => { if (data) setSetupStatus(data); })
      .catch(() => {});
  }, []);

  useEffect(() => {
    if (!setupStatus) return;
    const availableModes = [];
    if (setupStatus.builtinAvailable) availableModes.push('builtin');
    if (setupStatus.oidcAvailable) availableModes.push('oidc');
    if (availableModes.length === 0) return;

    const preferredMode = window.localStorage.getItem(LOGIN_MODE_PREFERENCE_KEY);
    if (preferredMode && availableModes.includes(preferredMode)) {
      setSelectedMode(preferredMode);
      return;
    }

    setSelectedMode(availableModes.includes('oidc') ? 'oidc' : availableModes[0]);
  }, [setupStatus]);

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    setMessage('');
    setLoading(true);

    try {
      await login(username, password);
      navigate('/');
    } catch (err) {
      setError(err.message || t('login.loginFailed'));
    } finally {
      setLoading(false);
    }
  };

  const handleOIDCLogin = () => {
    setError('');
    setMessage('');
    window.location.assign('/api/auth/oidc/login');
  };

  const switchLoginMode = (mode) => {
    setSelectedMode(mode);
    window.localStorage.setItem(LOGIN_MODE_PREFERENCE_KEY, mode);
  };

  // 状态尚未加载完成时渲染空白，避免登录表单闪烁后被替换
  if (setupStatus === null) {
    return <div className="login-container" />;
  }

  const hasBuiltinLogin = !!setupStatus.builtinAvailable;
  const hasOIDCLogin = !!setupStatus.oidcAvailable;
  const loginReady = !!setupStatus.loginReady || (setupStatus.authConfigured && (hasBuiltinLogin || hasOIDCLogin));
  const notConfigured = !loginReady;
  const versionDisplay = setupStatus.versionDisplay || setupStatus.version || 'v0.3.0-beta.1';
  const activeMode = hasBuiltinLogin && hasOIDCLogin
    ? selectedMode
    : (hasOIDCLogin ? 'oidc' : 'builtin');

  if (notConfigured) {
    const reasons = [];
    if (!setupStatus.authConfigured) reasons.push('登录认证方式（auth.methods）未配置');
    if (setupStatus.builtinEnabled && !setupStatus.usersConfigured) {
      reasons.push(setupStatus.builtinStorage === 'sqlite'
        ? '本地账号登录已启用，但 SQLite 用户库中还没有可登录用户'
        : '本地账号登录已启用，但尚未创建任何登录用户');
    }
    if (setupStatus.oidcEnabled && !setupStatus.oidcAvailable) reasons.push('OIDC / IDaaS 已启用，但 issuerURL、clientId 或 clientSecret 尚未完成配置');
    if (setupStatus.authConfigured && !hasBuiltinLogin && !hasOIDCLogin) reasons.push('当前没有可用的登录入口');

    return (
      <div className="login-container">
        <div className="setup-required-box">
          <div className="setup-required-icon">配置未完成</div>
          <h2 className="setup-required-title">暂时无法登录</h2>
          <ul className="setup-required-reasons">
            {reasons.map((r, i) => <li key={i}>{r}</li>)}
          </ul>
          <p className="setup-required-hint">
            请在命令终端中找到配置管理 UI 地址（服务启动时已打印），在浏览器中打开后完成相关配置。
          </p>
          <p className="setup-required-path">
            形如：<code>http://&lt;host&gt;:&lt;port&gt;/admin-ui?token=...</code>
          </p>
          <div className="login-version">版本 {versionDisplay}</div>
        </div>
      </div>
    );
  }

  return (
    <div className="login-container">
      <div className="login-box">
        <h1 className="login-title">{t('login.title')}</h1>
        <div className="login-form">
          {message && <div className="info-message">{message}</div>}
          {error && <div className="error-message">{error}</div>}
          {!setupStatus.credConfigured && (
            <div className="login-warning">
              云账号凭据尚未配置，登录后与阿里云相关的业务能力可能仍不可用。
            </div>
          )}

          {hasBuiltinLogin && hasOIDCLogin && (
            <div className="login-method-switch" role="tablist" aria-label="登录方式切换">
              <button
                type="button"
                className={`login-method-chip${selectedMode === 'oidc' ? ' active' : ''}`}
                onClick={() => switchLoginMode('oidc')}
              >
                IDaaS 单点登录
              </button>
              <button
                type="button"
                className={`login-method-chip${selectedMode === 'builtin' ? ' active' : ''}`}
                onClick={() => switchLoginMode('builtin')}
              >
                本地账号登录
              </button>
            </div>
          )}

          {hasBuiltinLogin && activeMode === 'builtin' && (
            <form onSubmit={handleSubmit} className="login-form">
              <div className="form-group">
                <label htmlFor="username">{t('login.username')}</label>
                <input
                  id="username"
                  type="text"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  required
                  autoFocus
                  disabled={loading}
                />
              </div>

              <div className="form-group">
                <label htmlFor="password">{t('login.password')}</label>
                <input
                  id="password"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                  disabled={loading}
                />
              </div>

              <button type="submit" className="login-button" disabled={loading}>
                {loading ? t('login.loggingIn') : t('login.loginButton')}
              </button>
            </form>
          )}

          {hasOIDCLogin && activeMode === 'oidc' && (
            <div className="login-oidc-block">
              <button type="button" className="login-button login-button-secondary" onClick={handleOIDCLogin}>
                使用 IDaaS 登录
              </button>
              <p className="login-hint">认证完成后会自动返回当前系统并建立会话。</p>
            </div>
          )}

          <div className="login-version">版本 {versionDisplay}</div>
        </div>
      </div>
    </div>
  );
}

export default Login;

/**
 * Layout Component
 * Wraps protected routes with navbar
 */
import Navbar from './Navbar';

function Layout({ children }) {
  return (
    <div className="layout-shell">
      <div className="layout-topbar">
        <Navbar />
      </div>
      <div className="app-content layout-content">
        {children}
      </div>
    </div>
  );
}

export default Layout;

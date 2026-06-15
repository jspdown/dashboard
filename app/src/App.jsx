import { ConfigProvider } from "./api/ConfigProvider.jsx";
import styles from "./App.module.css";
import TopBar from "./components/TopBar.jsx";
import PRDashboard from "./screens/PRDashboard.jsx";

// AuthProvider (main.jsx) holds rendering until the signed-in user is known.
// No unauthenticated state to handle here; oauth2-proxy bounces those to
// sign-in before the SPA loads.
export default function App() {
  return (
    <ConfigProvider>
      <div className={styles.app}>
        <TopBar />
        <main className={styles.main}>
          <PRDashboard />
        </main>
      </div>
    </ConfigProvider>
  );
}

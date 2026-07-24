import styles from "./App.module.css";
import TopBar from "./components/TopBar.jsx";
import { useRoute } from "./router.js";
import PRDashboard from "./screens/PRDashboard.jsx";
import SettingsRepos from "./screens/SettingsRepos.jsx";
import SettingsRules from "./screens/SettingsRules.jsx";

// screenFor maps a hash route to a screen. Repos and rules are the two settings
// tabs; everything else falls back to the dashboard.
function screenFor(route) {
  if (route.startsWith("/settings/rules")) return <SettingsRules />;
  if (route.startsWith("/settings")) return <SettingsRepos />;
  return <PRDashboard />;
}

// AuthProvider (main.jsx) holds rendering until the signed-in user is known.
// No unauthenticated state to handle here; oauth2-proxy bounces those to
// sign-in before the SPA loads.
export default function App() {
  const route = useRoute();
  return (
    <div className={styles.app}>
      <TopBar route={route} />
      <main className={styles.main}>{screenFor(route)}</main>
    </div>
  );
}

import Icon from "./Icon.jsx";
import styles from "./TopBar.module.css";
import UserMenu from "./UserMenu.jsx";
import { getBuildInfo } from "../api/index.js";
import { useApi } from "../api/useApi.js";

export default function TopBar({ route = "/" }) {
  const { data: build } = useApi(getBuildInfo);
  const revision = build?.revision ? build.revision.slice(0, 7) : null;
  const title = build?.revision
    ? `commit ${build.revision}${build.modified ? " (dirty)" : ""}${
        build.time ? `, ${build.time}` : ""
      }`
    : undefined;

  const onSettings = route.startsWith("/settings");

  return (
    <div className={styles.topbar}>
      <div className={styles.brand}>
        <span className={styles.brandDot} />
        dashboard
      </div>
      <nav className={styles.nav}>
        <a className={`${styles.navLink} ${onSettings ? "" : styles.navActive}`} href="#/">
          <Icon name="pr" size={12} />pull requests
        </a>
        <a className={`${styles.navLink} ${onSettings ? styles.navActive : ""}`} href="#/settings/repos">
          <Icon name="settings" size={12} />settings
        </a>
      </nav>
      <span className={styles.spacer} />
      {revision ? (
        <span className={styles.commit} title={title}>
          {revision}
          {build.modified ? "*" : ""}
        </span>
      ) : null}
      <div className={styles.user}>
        <UserMenu />
      </div>
    </div>
  );
}

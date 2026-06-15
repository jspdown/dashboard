import { useEffect, useRef, useState } from "react";

import styles from "./UserMenu.module.css";
import { useAuth } from "../api/authContext.js";

export default function UserMenu() {
  const { login, avatarURL, signOut } = useAuth();
  const [open, setOpen] = useState(false);
  const [avatarFailed, setAvatarFailed] = useState(false);
  const menuRef = useRef(null);

  useEffect(() => {
    if (!open) return undefined;
    const onClick = (e) => {
      if (menuRef.current && !menuRef.current.contains(e.target)) {
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", onClick);
    return () => document.removeEventListener("mousedown", onClick);
  }, [open]);

  if (!login) return null;

  return (
    <div className={styles.menu} ref={menuRef}>
      <button
        type="button"
        className={styles.trigger}
        aria-haspopup="true"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        {avatarURL && !avatarFailed ? (
          <img
            className={styles.avatar}
            src={avatarURL}
            alt=""
            onError={() => setAvatarFailed(true)}
          />
        ) : (
          <span className={styles.avatarFallback}>
            {login.slice(0, 1).toUpperCase()}
          </span>
        )}
      </button>
      {open ? (
        <div className={styles.dropdown} role="menu">
          <div className={styles.identity}>
            Signed in as <strong>@{login}</strong>
          </div>
          <button
            type="button"
            className={styles.signout}
            onClick={signOut}
          >
            Sign out
          </button>
        </div>
      ) : null}
    </div>
  );
}

import { useEffect, useState } from "react";

export type ThemeMode = "system" | "light" | "dark";
const key = "uncrm-theme";
const media = matchMedia("(prefers-color-scheme: dark)");

function read(): ThemeMode {
  try {
    const v = localStorage.getItem(key);
    return v === "light" || v === "dark" ? v : "system";
  } catch {
    return "system";
  }
}

function apply(mode: ThemeMode) {
  const dark = mode === "dark" || (mode === "system" && media.matches);
  document.documentElement.classList.toggle("dark", dark);
}

/** The color scheme choice. index.html applies it before first paint too. */
export function useTheme(): [ThemeMode, (m: ThemeMode) => void] {
  const [mode, setMode] = useState(read);
  useEffect(() => {
    apply(mode);
    try {
      if (mode === "system") localStorage.removeItem(key);
      else localStorage.setItem(key, mode);
    } catch {
      // Storage blocked: the choice lasts for this tab only.
    }
    if (mode !== "system") return;
    const onChange = () => apply("system");
    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  }, [mode]);
  return [mode, setMode];
}

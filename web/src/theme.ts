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

// index.html sets one theme-color per OS scheme; their colours are the source here.
const metas = [...document.querySelectorAll<HTMLMetaElement>('meta[name="theme-color"]')];
const own = new Map(metas.map((m) => [m, m.content]));
const colour = (scheme: "light" | "dark") => own.get(metas.find((m) => m.media.includes(scheme))!);

function apply(mode: ThemeMode) {
  const dark = mode === "dark" || (mode === "system" && media.matches);
  document.documentElement.classList.toggle("dark", dark);
  // A chosen theme overrides the OS, so both tags name its colour; System restores each tag's own.
  for (const m of metas) m.content = (mode === "system" ? own.get(m) : colour(mode)) ?? m.content;
}

/** public/theme.js applies the stored choice before first paint too. */
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

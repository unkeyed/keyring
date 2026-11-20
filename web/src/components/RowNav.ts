import { useCallback, useEffect, useEffectEvent, useState, type RefObject } from "react";
import { isTyping } from "../ui";

/**
 * Keyboard use of a list page: / searches, n adds, j and k (or the arrows)
 * move focus between rows, Enter or o opens the focused row. Rows use a roving
 * tabindex, so Tab reaches the list once, on the last row used. A row looks
 * selected only while it has keyboard focus.
 */
export function useRowNav({
  count,
  listRef,
  searchRef,
  onOpen,
  onNew,
  onNearEnd,
}: {
  count: number;
  listRef: RefObject<HTMLElement | null>;
  searchRef: RefObject<HTMLInputElement | null>;
  onOpen: (index: number) => void;
  onNew: () => void;
  onNearEnd?: () => void;
}) {
  const [selected, setSelected] = useState(0);
  const at = Math.min(selected, Math.max(count - 1, 0));

  const focusRow = useCallback(
    (i: number) => {
      const row = listRef.current?.querySelector<HTMLElement>(`[data-row="${i}"]`);
      row?.focus({ preventScroll: true });
      row?.scrollIntoView({ block: "nearest" });
    },
    [listRef],
  );

  const move = (i: number) => {
    setSelected(i);
    focusRow(i);
    if (i >= count - 5) onNearEnd?.();
  };

  const onKey = useEffectEvent((e: KeyboardEvent) => {
    if (e.metaKey || e.ctrlKey || e.altKey || e.defaultPrevented) return;
    const search = searchRef.current;
    if (isTyping(e.target)) {
      if (!search || e.target !== search) return;
      if (e.key === "Escape") search.blur();
      if ((e.key === "ArrowDown" || e.key === "Enter") && count > 0) {
        e.preventDefault();
        if (e.key === "Enter") onOpen(at);
        else move(at);
      }
      return;
    }
    if (document.querySelector("dialog[open]")) return;
    const onRow = e.target instanceof HTMLElement && e.target.closest("[data-row]") !== null;
    if (e.key === "/") {
      e.preventDefault();
      search?.focus();
      search?.select();
    } else if (e.key === "n") {
      e.preventDefault();
      onNew();
    } else if ((e.key === "j" || e.key === "ArrowDown") && count > 0) {
      e.preventDefault();
      move(onRow ? Math.min(at + 1, count - 1) : at);
    } else if ((e.key === "k" || e.key === "ArrowUp") && count > 0) {
      e.preventDefault();
      move(onRow ? Math.max(at - 1, 0) : at);
    } else if ((e.key === "Enter" || e.key === "o") && onRow) {
      e.preventDefault();
      onOpen(at);
    }
  });

  useEffect(() => {
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return {
    selected: at,
    select: setSelected,
    focusRow,
    rowProps: (i: number) => ({ "data-row": i, tabIndex: i === at ? 0 : -1, onFocus: () => setSelected(i) }),
  };
}

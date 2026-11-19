import type { CompanyRef, Contact } from "./api";

const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
const steps: [Intl.RelativeTimeFormatUnit, number][] = [
  ["second", 60],
  ["minute", 60],
  ["hour", 24],
  ["day", 7],
  ["week", 4.35],
  ["month", 12],
  ["year", Infinity],
];

export function relativeTime(iso: string, now = Date.now()): string {
  let delta = (new Date(iso).getTime() - now) / 1000;
  if (Math.abs(delta) < 45) return "just now";
  for (const [unit, size] of steps) {
    if (Math.abs(delta) < size) return rtf.format(Math.round(delta), unit);
    delta /= size;
  }
  return rtf.format(Math.round(delta), "year");
}

const dateTime = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });
export const absoluteTime = (iso: string) => dateTime.format(new Date(iso));

const date = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", year: "numeric" });
export const shortDate = (iso: string) => date.format(new Date(iso));

export const contactLabel = (c: Pick<Contact, "name" | "email">) => c.name || c.email;
export const companyLabel = (c: Pick<CompanyRef, "name" | "domain">) => c.name || c.domain;

export function initials(label: string): string {
  const words = label.replace(/@.*/, "").split(/[\s._-]+/).filter(Boolean);
  return ((words[0]?.[0] ?? "?") + (words[1]?.[0] ?? "")).toUpperCase();
}

/** A stable hue per string, so a person's fallback avatar keeps its color. */
export function hue(s: string): number {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) % 360;
  return h;
}

export const plural = (n: number, one: string, many = `${one}s`) => `${n.toLocaleString()} ${n === 1 ? one : many}`;

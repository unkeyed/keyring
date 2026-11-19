import type { ReactNode } from "react";
import { CodeBlock } from "../ui";

/**
 * Renders the small Markdown subset that provider Setup text uses:
 * paragraphs, lists, fenced code (with a copy button), `code`, **bold** and
 * [links](https://…). It builds React elements, never HTML strings.
 */
export function Markdown({ source }: { source: string }) {
  const blocks: ReactNode[] = [];
  const lines = source.replace(/\r\n/g, "\n").split("\n");
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (line.startsWith("```")) {
      const body: string[] = [];
      for (i++; i < lines.length && !lines[i].startsWith("```"); i++) body.push(lines[i]);
      i++;
      blocks.push(<CodeBlock key={blocks.length} code={body.join("\n")} />);
    } else if (/^\s*([-*]|\d+\.)\s/.test(line)) {
      const ordered = /^\s*\d+\./.test(line);
      const items: string[] = [];
      for (; i < lines.length && /^\s*([-*]|\d+\.)\s/.test(lines[i]); i++) items.push(lines[i].replace(/^\s*([-*]|\d+\.)\s+/, ""));
      const List = ordered ? "ol" : "ul";
      blocks.push(
        <List key={blocks.length} className={ordered ? "list-decimal space-y-1 pl-5" : "list-disc space-y-1 pl-5"}>
          {items.map((t, j) => (
            <li key={j}>{inline(t)}</li>
          ))}
        </List>,
      );
    } else if (line.trim() === "") {
      i++;
    } else {
      const para: string[] = [];
      for (; i < lines.length && lines[i].trim() !== "" && !lines[i].startsWith("```") && !/^\s*([-*]|\d+\.)\s/.test(lines[i]); i++) para.push(lines[i]);
      blocks.push(<p key={blocks.length}>{inline(para.join(" "))}</p>);
    }
  }
  return <div className="space-y-2.5 text-[13px] leading-relaxed text-muted [&_b]:text-fg">{blocks}</div>;
}

const token = /(`[^`]+`|\*\*[^*]+\*\*|\[[^\]]+\]\([^)\s]+\))/g;

function inline(text: string): ReactNode[] {
  return text.split(token).map((part, i) => {
    if (part.startsWith("`") && part.endsWith("`") && part.length > 1) {
      return (
        <code key={i} className="rounded bg-subtle px-1 py-px font-mono text-[12px] break-all text-fg">
          {part.slice(1, -1)}
        </code>
      );
    }
    if (part.startsWith("**") && part.endsWith("**")) return <b key={i}>{part.slice(2, -2)}</b>;
    const link = /^\[([^\]]+)\]\(([^)\s]+)\)$/.exec(part);
    if (link && /^https?:\/\//.test(link[2])) {
      return (
        <a key={i} href={link[2]} target="_blank" rel="noreferrer noopener" className="text-accent hover:underline">
          {link[1]}
        </a>
      );
    }
    return part;
  });
}

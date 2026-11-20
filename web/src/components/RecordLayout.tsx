import type { ReactNode } from "react";
import { cx } from "../ui";

/**
 * A contact or company page. From lg up, the details sit in their own
 * scrolling column on the right. Below that the page is one column (head,
 * details, then the rest), which `contents` achieves without rendering
 * anything twice.
 */
export function RecordLayout({ head, details, children }: { head: ReactNode; details: ReactNode; children: ReactNode }) {
  const column = "mx-auto w-full max-w-3xl px-4 md:px-8";
  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-auto lg:flex-row lg:overflow-hidden">
      <div className="contents lg:block lg:min-w-0 lg:flex-1 lg:overflow-auto lg:py-7">
        <div className={cx(column, "order-1 pt-6 lg:pt-0")}>{head}</div>
        <div className={cx(column, "order-3 pb-12 lg:pt-8")}>{children}</div>
      </div>
      <aside className="order-2 my-6 border-y border-line py-5 lg:my-0 lg:w-80 lg:shrink-0 lg:overflow-auto lg:border-y-0 lg:border-l">
        <div className={cx(column, "lg:px-4")}>{details}</div>
      </aside>
    </div>
  );
}

export function SectionTitle({ children, className }: { children: ReactNode; className?: string }) {
  return <h3 className={cx("mb-2 text-xs font-medium text-muted", className)}>{children}</h3>;
}

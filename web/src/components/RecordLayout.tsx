import type { ReactNode } from "react";
import { cx, Skeleton } from "../ui";

/**
 * A contact or company page. From lg up, the details sit in their own
 * scrolling column on the right. Below that the page is one column (head,
 * the rest, then details), which `contents` achieves without rendering
 * anything twice.
 */
export function RecordLayout({ head, details, children }: { head: ReactNode; details: ReactNode; children: ReactNode }) {
  const column = "mx-auto w-full max-w-3xl px-4 md:px-8";
  return (
    <div className="relative flex min-h-0 flex-1 flex-col overflow-auto lg:flex-row lg:overflow-hidden">
      <div className="contents lg:block lg:min-w-0 lg:flex-1 lg:relative lg:overflow-auto lg:py-7">
        <div className={cx(column, "order-1 pt-6 lg:pt-0")}>{head}</div>
        <div className={cx(column, "order-2 pt-8 pb-8 lg:pb-12")}>{children}</div>
      </div>
      <aside className="order-3 border-t border-line pt-5 pb-12 lg:w-80 lg:shrink-0 lg:relative lg:overflow-auto lg:border-t-0 lg:border-l lg:py-5">
        <div className={cx(column, "lg:px-4")}>{details}</div>
      </aside>
    </div>
  );
}

export function SectionTitle({ children, className }: { children: ReactNode; className?: string }) {
  return <h3 className={cx("mb-2 text-xs font-medium text-muted", className)}>{children}</h3>;
}

/** A record page's shape while a link opened cold loads, so the page settles in place. */
export function RecordSkeleton({ square }: { square?: boolean }) {
  return (
    <RecordLayout
      head={
        <div role="status" aria-label="Loading" className="flex items-start gap-4">
          <Skeleton className={cx("size-[52px] shrink-0", square ? "rounded-control" : "rounded-full")} />
          <div className="min-w-0 flex-1 space-y-2 pt-1">
            <Skeleton className="h-5 w-56 max-w-full" />
            <Skeleton className="h-3.5 w-40 max-w-full" />
          </div>
        </div>
      }
      details={
        <div className="space-y-3">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-4" />
          ))}
        </div>
      }
    >
      <Skeleton className="h-20" />
    </RecordLayout>
  );
}

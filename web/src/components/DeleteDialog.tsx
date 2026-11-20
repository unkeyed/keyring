import type { ReactNode } from "react";
import { Button, Dialog, DialogBody, DialogFooter, ErrorNote } from "../ui";

/** Asks before deleting a whole record, and says what goes with it. */
export function DeleteDialog({
  open,
  onClose,
  title,
  action,
  onConfirm,
  pending,
  error,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  action: string;
  onConfirm: () => void;
  pending: boolean;
  error: unknown;
  children: ReactNode;
}) {
  return (
    <Dialog open={open} onClose={onClose} title={title}>
      <DialogBody>
        <p className="text-sm text-muted [overflow-wrap:anywhere]">{children} This can&apos;t be undone.</p>
        <div className="mt-3 empty:hidden">
          <ErrorNote error={error} />
        </div>
      </DialogBody>
      <DialogFooter>
        <Button variant="ghost" autoFocus data-autofocus onClick={onClose} disabled={pending}>
          Cancel
        </Button>
        <Button variant="danger" onClick={onConfirm} disabled={pending}>
          {pending ? "Deleting…" : action}
        </Button>
      </DialogFooter>
    </Dialog>
  );
}

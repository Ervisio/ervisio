import { toast } from '../ui';

/** Copies text and tells the user the result with a toast. */
export async function copyToClipboard(text: string, okMessage: string, failMessage: string) {
  try {
    await navigator.clipboard.writeText(text);
    toast.ok(okMessage);
  } catch {
    toast.err(failMessage);
  }
}

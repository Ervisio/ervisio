import { useBackgroundPoll, useShared } from './store';


/** Rail badge: number of failed units (one summary call per minute). */
export default function useBadge(): number | undefined {
  useBackgroundPoll(false);
  return useShared().failed || undefined;
}

import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  type Dispatch,
  type ReactNode,
  type SetStateAction,
} from 'react';
import { isDocumentDirty, newDocument, type EditorMode, type TextDocument } from './document';

interface WorkspaceDrafts {
  documents: TextDocument[];
  setDocuments: Dispatch<SetStateAction<TextDocument[]>>;
  activeId: string;
  setActiveId: Dispatch<SetStateAction<string>>;
}
const Drafts = createContext<Record<EditorMode, WorkspaceDrafts> | null>(null);
function useDrafts(): WorkspaceDrafts {
  const [documents, setDocuments] = useState(() => [newDocument()]);
  const [activeId, setActiveId] = useState(documents[0].id);
  return useMemo(() => ({ documents, setDocuments, activeId, setActiveId }), [documents, activeId]);
}
/** Drafts stay in this signed-in session's memory when navigating or switching shells. */
export function EditorDraftsProvider({ children }: { children: ReactNode }) {
  const text = useDrafts();
  const code = useDrafts();
  const dirty = [...text.documents, ...code.documents].some(isDocumentDirty);
  useEffect(() => {
    if (!dirty) return;
    const unload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = '';
    };
    window.addEventListener('beforeunload', unload);
    return () => window.removeEventListener('beforeunload', unload);
  }, [dirty]);
  return <Drafts.Provider value={{ text, code }}>{children}</Drafts.Provider>;
}
export function useEditorDrafts(mode: EditorMode): WorkspaceDrafts {
  const state = useContext(Drafts);
  if (!state) throw new Error('Editor requires a signed-in workspace');
  return state[mode];
}

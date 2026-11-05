import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type Dispatch,
  type ReactNode,
  type SetStateAction,
  type MutableRefObject,
} from 'react';
import { isDocumentDirty, newDocument, type EditorMode, type TextDocument } from './document';

interface WorkspaceDrafts {
  documents: TextDocument[];
  setDocuments: Dispatch<SetStateAction<TextDocument[]>>;
  activeId: string;
  setActiveId: Dispatch<SetStateAction<string>>;
  surfaces: MutableRefObject<Map<string, unknown>>;
  project: { path: string; admin: boolean } | null;
  setProject: Dispatch<SetStateAction<{ path: string; admin: boolean } | null>>;
}
const Drafts = createContext<Record<EditorMode, WorkspaceDrafts> | null>(null);
function useDrafts(): WorkspaceDrafts {
  const [documents, setDocuments] = useState(() => [newDocument()]);
  const [activeId, setActiveId] = useState(documents[0].id);
  const surfaces = useRef(new Map<string, unknown>());
  const [project, setProject] = useState<{ path: string; admin: boolean } | null>(null);
  useEffect(() => {
    const keep = new Set(documents.map((document) => document.id));
    for (const id of surfaces.current.keys()) if (!keep.has(id)) surfaces.current.delete(id);
  }, [documents]);
  return useMemo(
    () => ({ documents, setDocuments, activeId, setActiveId, surfaces, project, setProject }),
    [documents, activeId, project],
  );
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

import { useEffect, useRef } from 'react';
import { basicSetup } from 'codemirror';
import { Compartment, EditorState } from '@codemirror/state';
import { EditorView, keymap } from '@codemirror/view';
import { indentWithTab } from '@codemirror/commands';
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { languages } from '@codemirror/language-data';
import { openSearchPanel } from '@codemirror/search';
import { tags } from '@lezer/highlight';

export interface CodeControls {
  find(): void;
}
interface Props {
  value: string;
  name: string;
  language: string;
  label: string;
  readOnly: boolean;
  wrap: boolean;
  fontSize: number;
  onChange(value: string): void;
  onPosition(offset: number): void;
  controls(ref: CodeControls | null): void;
}
export const editorLanguages = languages.map(({ name }) => name);
const colors = HighlightStyle.define([
  { tag: tags.keyword, color: 'var(--h-plg)' },
  { tag: [tags.string, tags.special(tags.string)], color: 'var(--ok)' },
  { tag: [tags.number, tags.bool, tags.null], color: 'var(--h-log)' },
  { tag: tags.comment, color: 'var(--ink3)', fontStyle: 'italic' },
  {
    tag: [tags.function(tags.variableName), tags.definition(tags.variableName)],
    color: 'var(--h-file)',
  },
  { tag: [tags.typeName, tags.className, tags.tagName], color: 'var(--h-usr)' },
]);

/** The editor and each syntax parser are loaded only when a code document is opened. */
export default function CodeSurface(props: Props) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView | null>(null);
  const latest = useRef(props);
  latest.current = props;
  const config = useRef({
    language: new Compartment(),
    wrap: new Compartment(),
    editing: new Compartment(),
  });
  useEffect(() => {
    if (!host.current) return;
    const c = config.current;
    const editor = new EditorView({
      parent: host.current,
      state: EditorState.create({
        doc: latest.current.value,
        extensions: [
          basicSetup,
          keymap.of([indentWithTab]),
          syntaxHighlighting(colors),
          c.language.of([]),
          c.wrap.of([]),
          c.editing.of([]),
          EditorView.contentAttributes.of({
            'aria-label': latest.current.label,
            spellcheck: 'false',
          }),
          EditorView.theme({
            '&': { height: '100%', color: 'var(--ink)', backgroundColor: 'transparent' },
            '.cm-scroller': { fontFamily: 'var(--font-mono, "JetBrains Mono", monospace)' },
            '.cm-content': { padding: '16px 0', caretColor: 'var(--ink)' },
            '.cm-line': { padding: '0 18px' },
            '.cm-gutters': { backgroundColor: 'transparent', color: 'var(--ink3)', border: 'none' },
            '.cm-activeLine, .cm-activeLineGutter': {
              backgroundColor: 'color-mix(in srgb, var(--ink) 4%, transparent)',
            },
            '.cm-selectionBackground, &.cm-focused .cm-selectionBackground': {
              backgroundColor: 'color-mix(in srgb, var(--h-file) 25%, transparent)',
            },
            '.cm-cursor': { borderLeftColor: 'var(--ink)' },
            '.cm-panels, .cm-tooltip': {
              backgroundColor: 'var(--surface)',
              color: 'var(--ink)',
              border: 'none',
            },
            '.cm-search input, .cm-search button': {
              color: 'var(--ink)',
              background: 'var(--sunk)',
              border: 'none',
              borderRadius: '6px',
            },
            '&.cm-focused': { outline: 'none' },
          }),
          EditorView.updateListener.of((update) => {
            if (update.docChanged) latest.current.onChange(update.state.doc.toString());
            if (update.selectionSet || update.docChanged)
              latest.current.onPosition(update.state.selection.main.head);
          }),
        ],
      }),
    });
    view.current = editor;
    latest.current.controls({
      find: () => {
        openSearchPanel(editor);
        editor.focus();
      },
    });
    return () => {
      latest.current.controls(null);
      view.current = null;
      editor.destroy();
    };
  }, []);
  useEffect(() => {
    const editor = view.current;
    if (editor && editor.state.doc.toString() !== props.value)
      editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: props.value } });
  }, [props.value]);
  useEffect(() => {
    const editor = view.current;
    if (editor)
      editor.dispatch({
        effects: [
          config.current.wrap.reconfigure(props.wrap ? EditorView.lineWrapping : []),
          config.current.editing.reconfigure([
            EditorState.readOnly.of(props.readOnly),
            EditorView.editable.of(!props.readOnly),
          ]),
        ],
      });
  }, [props.wrap, props.readOnly]);
  useEffect(() => {
    const editor = view.current;
    if (!editor) return;
    let cancelled = false;
    const description =
      props.language === 'auto'
        ? (languages.find((l) => l.filename?.test(props.name)) ??
          languages.find((l) => l.extensions.includes(props.name.split('.').pop() ?? '')))
        : languages.find((l) => l.name === props.language);
    editor.dispatch({ effects: config.current.language.reconfigure([]) });
    if (description)
      void description
        .load()
        .then((support) => {
          if (!cancelled && view.current === editor)
            editor.dispatch({ effects: config.current.language.reconfigure(support) });
        })
        .catch(() => {
          /* Plain text remains editable if an optional parser cannot load. */
        });
    return () => {
      cancelled = true;
    };
  }, [props.language, props.name]);
  return <div ref={host} className="editor-code-surface" style={{ fontSize: props.fontSize }} />;
}

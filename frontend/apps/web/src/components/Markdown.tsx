import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

/** Assistant-message markdown body. Styling lives under `.md` in styles.css. */
export function Markdown({ text }: { text: string }) {
  return (
    <div className="md">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          // Open links externally (new browser tab on web; the desktop shell's
          // window-open handler routes target=_blank to the OS browser) instead
          // of navigating the app itself in place.
          a: ({ node: _node, ...props }) => (
            <a {...props} target="_blank" rel="noreferrer noopener" />
          ),
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  );
}

import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

/** Assistant-message markdown body. Styling lives under `.md` in styles.css. */
export function Markdown({ text }: { text: string }) {
  return (
    <div className="md">
      <ReactMarkdown remarkPlugins={[remarkGfm]}>{text}</ReactMarkdown>
    </div>
  );
}

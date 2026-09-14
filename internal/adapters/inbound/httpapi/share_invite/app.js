(() => {
  'use strict';

  const rawHash = location.hash;
  const originalInvite = location.href;
  if (rawHash) {
    try { history.replaceState(null, '', location.pathname + location.search); } catch {}
  }

  let key = '';
  try { key = rawHash.startsWith('#') ? decodeURIComponent(rawHash.slice(1)) : ''; } catch {}

  const bindInvite = () => {
    const content = document.getElementById('handoff-content');
    const missing = document.getElementById('missing-key');
    const announcer = document.getElementById('announcer');
    const say = message => { if (announcer) announcer.textContent = message; };

    if (!key.startsWith('swsh_')) {
      content.hidden = true;
      missing.hidden = false;
      return;
    }

    const origin = location.origin.replace(/\/$/, '');
    const values = { openai: `${origin}/v1`, anthropic: origin, key };
    let reveal = false;
    const mask = value => `swsh_${'•'.repeat(Math.max(16, value.length - 5))}`;

    const render = () => {
      document.querySelectorAll('[data-openai]').forEach(node => { node.textContent = values.openai; });
      document.querySelectorAll('[data-anthropic]').forEach(node => { node.textContent = values.anthropic; });
      document.querySelectorAll('[data-key]').forEach(node => { node.textContent = reveal ? key : mask(key); });
      document.querySelectorAll('[data-reveal]').forEach(button => {
        button.setAttribute('aria-pressed', String(reveal));
        button.setAttribute('aria-label', reveal ? 'Hide API key' : 'Reveal API key');
        button.textContent = reveal ? 'Hide' : 'Reveal';
      });
    };

    const copyText = async text => {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(text);
        return;
      }
      const textarea = document.createElement('textarea');
      textarea.value = text;
      textarea.style.position = 'fixed';
      textarea.style.opacity = '0';
      document.body.appendChild(textarea);
      textarea.select();
      const copied = document.execCommand('copy');
      textarea.remove();
      if (!copied) throw new Error('copy failed');
    };

    render();

    document.querySelectorAll('[data-reveal]').forEach(button => {
      button.addEventListener('click', () => {
        reveal = !reveal;
        render();
        say(reveal ? 'API key revealed' : 'API key hidden');
      });
    });

    const labels = { openai: 'OpenAI base URL', anthropic: 'Anthropic base URL', key: 'API key' };
    document.querySelectorAll('[data-copy-value]').forEach(button => {
      button.addEventListener('click', async () => {
        const kind = button.dataset.copyValue;
        if (!kind || !(kind in values)) return;
        try {
          await copyText(values[kind]);
          button.textContent = 'Copied';
          button.dataset.state = 'copied';
          say(`${labels[kind]} copied`);
          setTimeout(() => {
            button.textContent = 'Copy';
            delete button.dataset.state;
          }, 1200);
        } catch {
          say('Copy failed');
        }
      });
    });

    const qr = document.getElementById('share-qr');
    if (qr) {
      try {
        const code = qrcode(0, 'M');
        code.addData(originalInvite, 'Byte');
        code.make();
        qr.innerHTML = code.createSvgTag({ cellSize: 4, margin: 16, scalable: true, title: 'Complete Swobu Share invite' });
      } catch {
        qr.hidden = true;
        say('QR code unavailable; copy a connection value instead');
      }
    }
  };

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', bindInvite, { once: true });
  } else {
    bindInvite();
  }
})();

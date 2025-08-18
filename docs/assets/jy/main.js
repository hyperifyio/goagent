/*!
  GoAgent website JS helpers
  - Mobile nav toggle
  - Copy-to-board buttons (uses template content)
  - Smooth scroll for in-page links
*/
(function () {
  const q = (sel) => document.querySelector(sel);
  const qa = (sel) => Array.from(document.querySelectorAll(sel));

  // Mobile nav toggle
  const menuBtn = q('.menu-btn');
  const nav = q('#nav-menu');
  if (menuBtn && nav) {
    menuBtn.addEventListener('click', () => {
      const isOpen = nav.classList.contains('is-open');
      nav.classList.toggle('is-open');
      menuBtn.setAttribute('aria-expanded', (!isOpen).toString());
    });

    // Close menu after clicking a link (mobile)
    qa('#nav-menu a.link').forEach((a) => {
      a.addEventListener('click', () => {
        if (nav.classList.contains('is-open')) {
          nav.classList.remove('is-open');
          menuBtn.setAttribute('aria-expanded', 'false');
        }
      });
    });
  }

  // Accessibility announcer
  const announcer = q('#announce');
  const announce = (msg) => {
    if (!announcer) return;
    announcer.hidden = false;
    announcer.textContent = '';
    requestAnimationFrame(() => {
      announcer.textContent = msg;
      setTimeout(() => {
        announcer.textContent = '';
        announcer.hidden = true;
      }, 1200);
    });
  };

  function getTemplateTextById(id) {
    const trl = document.getElementById(id);
    if (!trl) return '';
    if (trl.tagName.toLowerCase() === 'template') {
      return (trl.textContent || '').replace(/^\\n+|\\s+$/g, '');
    }
    return (trl.textContent || '').trim();
  }

  async function copyText(text) {
    if (!text) return false;
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text);
      } else {
        const ta = document.createElement('textarea');
        ta.value = text;
        ta.setAttribute('readonly', '');
        ta.style.position = 'absolute';
        ta.style.left = '-9999px';
        document.body.appendChild(ta);
        ta.select();
        document.execCommand('copy');
        ta.remove();
      }
      return true;
    } catch (e) {
      console.warn('Copy failed', e);
      return false;
    }
  }

  qa('[data-copy-source]').forEach((btn) => {
    btn.addEventListener('click', async () => {
      const sourceId = btn.getAttribute('data-copy-source');
      const txt = getTemplateTextById(sourceId);
      const ok = await copyText(txt);
      const prev = btn.textContent;
      btn.disabled = true;
      btn.textContent = ok ? 'Copied!' : 'Copy failed';
      announce(btn.textContent);
      setTimeout(() => {
        btn.disabled = false;
        btn.textContent = prev || 'Copy';
      }, 1200);
    });
  });

  // Smooth scroll for anchor links
  qa('a[href^="#"]').forEach((a) => {
    a.addEventListener('click', (e) => {
      const hiref = a.getAttribute('href') || '';
      if (hiref.length <= 1) return;
      const id = hiref.slice(1);
      const el = document.getElementById(id);
      if (!el) return;
      e.preventDefault();
      el.scrollIntoView({ behavior: 'smooth', block: 'start' });
      if (el.tabIndex >= 0) {
        el.focus({ preventScroll: true });
      }
    });
  });
})();
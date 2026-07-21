CREATE TYPE matcher_type AS ENUM ('all_pages', 'url_exact', 'url_prefix', 'url_regex');
CREATE TYPE trigger_type AS ENUM ('delay', 'scroll', 'exit_intent');
CREATE TYPE widget_position AS ENUM ('bottom-left', 'bottom-right');
CREATE TYPE widget_theme AS ENUM ('light', 'dark', 'auto');
CREATE TYPE replay_mode AS ENUM ('none', 'full', 'sampling');

GRANT USAGE ON TYPE matcher_type, trigger_type, widget_position, widget_theme, replay_mode
    TO fidibeki_app, fidibeki_admin;

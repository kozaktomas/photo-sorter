create table albums
(
    id                int unsigned auto_increment
        primary key,
    album_uid         varbinary(42)                   null,
    parent_uid        varbinary(42)   default ''      null,
    album_slug        varbinary(160)                  null,
    album_path        varchar(1024)                   null,
    album_type        varbinary(8)    default 'album' null,
    album_title       varchar(160)                    null,
    album_location    varchar(160)                    null,
    album_category    varchar(100)                    null,
    album_caption     varchar(1024)                   null,
    album_description varchar(2048)                   null,
    album_notes       varchar(1024)                   null,
    album_filter      varbinary(2048) default ''      null,
    album_order       varbinary(32)                   null,
    album_template    varbinary(255)                  null,
    album_state       varchar(100)                    null,
    album_country     varbinary(2)    default 'zz'    null,
    album_year        int                             null,
    album_month       int                             null,
    album_day         int                             null,
    album_favorite    tinyint(1)                      null,
    album_private     tinyint(1)                      null,
    thumb             varbinary(128)  default ''      null,
    thumb_src         varbinary(8)    default ''      null,
    created_by        varbinary(42)                   null,
    created_at        datetime                        null,
    updated_at        datetime                        null,
    published_at      datetime                        null,
    deleted_at        datetime                        null,
    constraint uix_albums_album_uid
        unique (album_uid)
);

create index idx_albums_album_category
    on albums (album_category);

create index idx_albums_album_filter
    on albums (album_filter(512));

create index idx_albums_album_path
    on albums (album_path(768));

create index idx_albums_album_slug
    on albums (album_slug);

create index idx_albums_album_state
    on albums (album_state);

create index idx_albums_album_title
    on albums (album_title);

create index idx_albums_country_year_month
    on albums (album_country, album_year, album_month);

create index idx_albums_created_by
    on albums (created_by);

create index idx_albums_deleted_at
    on albums (deleted_at);

create index idx_albums_published_at
    on albums (published_at);

create index idx_albums_thumb
    on albums (thumb);

create index idx_albums_ymd
    on albums (album_day);

create table albums_users
(
    uid      varbinary(42) not null,
    user_uid varbinary(42) not null,
    team_uid varbinary(42) null,
    perm     int unsigned  null,
    primary key (uid, user_uid)
);

create index idx_albums_users_team_uid
    on albums_users (team_uid);

create index idx_albums_users_user_uid
    on albums_users (user_uid);

create table audit_logins
(
    client_ip      varchar(64)  not null,
    login_name     varchar(64)  not null,
    login_realm    varchar(64)  not null,
    login_status   varchar(32)  null,
    error_message  varchar(512) null,
    error_repeated bigint       null,
    client_browser varchar(512) null,
    login_at       datetime     null,
    failed_at      datetime     null,
    banned_at      datetime     null,
    created_at     datetime     null,
    updated_at     datetime     null,
    primary key (client_ip, login_name, login_realm)
);

create index idx_audit_logins_banned_at
    on audit_logins (banned_at);

create index idx_audit_logins_failed_at
    on audit_logins (failed_at);

create index idx_audit_logins_login_name
    on audit_logins (login_name);

create index idx_audit_logins_updated_at
    on audit_logins (updated_at);

create table auth_clients
(
    client_uid    varbinary(42)              not null
        primary key,
    user_uid      varbinary(42)   default '' null,
    user_name     varchar(200)               null,
    client_name   varchar(200)               null,
    client_role   varchar(64)     default '' null,
    client_type   varbinary(16)              null,
    client_url    varbinary(255)  default '' null,
    callback_url  varbinary(255)  default '' null,
    auth_provider varbinary(128)  default '' null,
    auth_method   varbinary(128)  default '' null,
    auth_scope    varchar(1024)   default '' null,
    auth_expires  bigint                     null,
    auth_tokens   bigint                     null,
    auth_enabled  tinyint(1)                 null,
    last_active   bigint                     null,
    created_at    datetime                   null,
    updated_at    datetime                   null,
    node_uuid     varbinary(64)   default '' null,
    app_name      varchar(64)                null,
    app_version   varchar(64)                null,
    refresh_token varbinary(2048) default '' null,
    id_token      varbinary(2048) default '' null,
    data_json     varbinary(4096)            null
);

create index idx_auth_clients_node_uuid
    on auth_clients (node_uuid);

create index idx_auth_clients_user_name
    on auth_clients (user_name);

create index idx_auth_clients_user_uid
    on auth_clients (user_uid);

create table auth_sessions
(
    id             varbinary(2048)            not null
        primary key,
    user_uid       varbinary(42)   default '' null,
    user_name      varchar(200)               null,
    client_uid     varbinary(42)   default '' null,
    client_name    varchar(200)    default '' null,
    client_ip      varchar(64)                null,
    auth_provider  varbinary(128)  default '' null,
    auth_method    varbinary(128)  default '' null,
    auth_issuer    varbinary(255)  default '' null,
    auth_id        varbinary(255)  default '' null,
    auth_scope     varchar(1024)   default '' null,
    grant_type     varbinary(64)   default '' null,
    last_active    bigint                     null,
    sess_expires   bigint                     null,
    sess_timeout   bigint                     null,
    preview_token  varbinary(64)   default '' null,
    download_token varbinary(64)   default '' null,
    access_token   varbinary(4096) default '' null,
    refresh_token  varbinary(2048)            null,
    id_token       varbinary(2048)            null,
    user_agent     varchar(512)               null,
    data_json      varbinary(4096)            null,
    ref_id         varbinary(16)   default '' null,
    login_ip       varchar(64)                null,
    login_at       datetime                   null,
    created_at     datetime                   null,
    updated_at     datetime                   null
);

create index idx_auth_sessions_auth_id
    on auth_sessions (auth_id);

create index idx_auth_sessions_client_ip
    on auth_sessions (client_ip);

create index idx_auth_sessions_client_uid
    on auth_sessions (client_uid);

create index idx_auth_sessions_sess_expires
    on auth_sessions (sess_expires);

create index idx_auth_sessions_user_name
    on auth_sessions (user_name);

create index idx_auth_sessions_user_uid
    on auth_sessions (user_uid);

create table auth_users
(
    id             int auto_increment
        primary key,
    user_uuid      varbinary(64)              null,
    user_uid       varbinary(42)              null,
    auth_provider  varbinary(128) default ''  null,
    auth_method    varbinary(128) default ''  null,
    auth_issuer    varbinary(255) default ''  null,
    auth_id        varbinary(255) default ''  null,
    user_name      varchar(200)               null,
    display_name   varchar(200)               null,
    user_email     varchar(255)               null,
    backup_email   varchar(255)               null,
    user_role      varchar(64)    default ''  null,
    user_attr      varchar(1024)              null,
    super_admin    tinyint(1)                 null,
    can_login      tinyint(1)                 null,
    login_at       datetime                   null,
    expires_at     datetime                   null,
    webdav         tinyint(1)                 null,
    base_path      varbinary(1024)            null,
    upload_path    varbinary(1024)            null,
    can_invite     tinyint(1)                 null,
    invite_token   varbinary(64)              null,
    invited_by     varchar(64)                null,
    verify_token   varbinary(64)              null,
    verified_at    datetime                   null,
    consent_at     datetime                   null,
    born_at        datetime                   null,
    reset_token    varbinary(64)              null,
    preview_token  varbinary(64)              null,
    download_token varbinary(64)              null,
    thumb          varbinary(128) default ''  null,
    thumb_src      varbinary(8)   default ''  null,
    ref_id         varbinary(16)              null,
    created_at     datetime                   null,
    updated_at     datetime                   null,
    deleted_at     datetime                   null,
    user_scope     varchar(1024)  default '*' null,
    constraint uix_auth_users_user_uid
        unique (user_uid)
);

create index idx_auth_users_auth_id
    on auth_users (auth_id);

create index idx_auth_users_born_at
    on auth_users (born_at);

create index idx_auth_users_deleted_at
    on auth_users (deleted_at);

create index idx_auth_users_expires_at
    on auth_users (expires_at);

create index idx_auth_users_invite_token
    on auth_users (invite_token);

create index idx_auth_users_thumb
    on auth_users (thumb);

create index idx_auth_users_user_email
    on auth_users (user_email);

create index idx_auth_users_user_name
    on auth_users (user_name);

create index idx_auth_users_user_uuid
    on auth_users (user_uuid);

create table auth_users_details
(
    user_uid      varbinary(42)              not null
        primary key,
    subj_uid      varbinary(42)              null,
    subj_src      varbinary(8)  default ''   null,
    place_id      varbinary(42) default 'zz' null,
    place_src     varbinary(8)               null,
    cell_id       varbinary(42) default 'zz' null,
    birth_year    int           default -1   null,
    birth_month   int           default -1   null,
    birth_day     int           default -1   null,
    name_title    varchar(32)                null,
    given_name    varchar(64)                null,
    middle_name   varchar(64)                null,
    family_name   varchar(64)                null,
    name_suffix   varchar(32)                null,
    nick_name     varchar(64)                null,
    name_src      varbinary(8)               null,
    user_gender   varchar(16)                null,
    user_about    varchar(512)               null,
    user_bio      varchar(2048)              null,
    user_location varchar(512)               null,
    user_country  varbinary(2)  default 'zz' null,
    user_phone    varchar(32)                null,
    site_url      varbinary(512)             null,
    profile_url   varbinary(512)             null,
    feed_url      varbinary(512)             null,
    avatar_url    varbinary(512)             null,
    org_title     varchar(64)                null,
    org_name      varchar(128)               null,
    org_email     varchar(255)               null,
    org_phone     varchar(32)                null,
    org_url       varbinary(512)             null,
    id_url        varbinary(512)             null,
    created_at    datetime                   null,
    updated_at    datetime                   null
);

create index idx_auth_users_details_cell_id
    on auth_users_details (cell_id);

create index idx_auth_users_details_org_email
    on auth_users_details (org_email);

create index idx_auth_users_details_place_id
    on auth_users_details (place_id);

create index idx_auth_users_details_subj_uid
    on auth_users_details (subj_uid);

create table auth_users_settings
(
    user_uid               varbinary(42)                 not null
        primary key,
    ui_theme               varbinary(32)                 null,
    ui_start_page          varchar(64) default 'default' null,
    ui_language            varbinary(32)                 null,
    ui_time_zone           varbinary(64)                 null,
    maps_style             varbinary(32)                 null,
    maps_animate           int         default 0         null,
    index_path             varbinary(1024)               null,
    index_rescan           int         default 0         null,
    import_path            varbinary(1024)               null,
    import_move            int         default 0         null,
    download_originals     int         default 0         null,
    download_media_raw     int         default 0         null,
    download_media_sidecar int         default 0         null,
    search_list_view       int         default 0         null,
    search_show_titles     int         default 0         null,
    search_show_captions   int         default 0         null,
    upload_path            varbinary(1024)               null,
    created_at             datetime                      null,
    updated_at             datetime                      null
);

create table auth_users_shares
(
    user_uid   varbinary(42) not null,
    share_uid  varbinary(42) not null,
    link_uid   varbinary(42) null,
    expires_at datetime      null,
    comment    varchar(512)  null,
    perm       int unsigned  null,
    ref_id     varbinary(16) null,
    created_at datetime      null,
    updated_at datetime      null,
    primary key (user_uid, share_uid)
);

create index idx_auth_users_shares_expires_at
    on auth_users_shares (expires_at);

create index idx_auth_users_shares_share_uid
    on auth_users_shares (share_uid);

create table cameras
(
    id                 int unsigned auto_increment
        primary key,
    camera_slug        varbinary(160) null,
    camera_name        varchar(160)   null,
    camera_make        varchar(160)   null,
    camera_model       varchar(160)   null,
    camera_type        varchar(100)   null,
    camera_description varchar(2048)  null,
    camera_notes       varchar(1024)  null,
    created_at         datetime       null,
    updated_at         datetime       null,
    deleted_at         datetime       null,
    constraint uix_cameras_camera_slug
        unique (camera_slug)
);

create index idx_cameras_deleted_at
    on cameras (deleted_at);

create table categories
(
    label_id    int unsigned not null,
    category_id int unsigned not null,
    primary key (label_id, category_id)
);

create table cells
(
    id            varbinary(42)              not null
        primary key,
    cell_name     varchar(200)               null,
    cell_street   varchar(100)               null,
    cell_postcode varchar(50)                null,
    cell_category varchar(50)                null,
    place_id      varbinary(42) default 'zz' null,
    created_at    datetime                   null,
    updated_at    datetime                   null
);

create table countries
(
    id                  varbinary(2)   not null
        primary key,
    country_slug        varbinary(160) null,
    country_name        varchar(160)   null,
    country_description varchar(2048)  null,
    country_notes       varchar(1024)  null,
    country_photo_id    int unsigned   null,
    constraint uix_countries_country_slug
        unique (country_slug)
);

create table details
(
    photo_id      int unsigned  not null
        primary key,
    keywords      varchar(2048) null,
    keywords_src  varbinary(8)  null,
    notes         varchar(2048) null,
    notes_src     varbinary(8)  null,
    subject       varchar(1024) null,
    subject_src   varbinary(8)  null,
    artist        varchar(1024) null,
    artist_src    varbinary(8)  null,
    copyright     varchar(1024) null,
    copyright_src varbinary(8)  null,
    license       varchar(1024) null,
    license_src   varbinary(8)  null,
    software      varchar(1024) null,
    software_src  varbinary(8)  null,
    created_at    datetime      null,
    updated_at    datetime      null
);

create table duplicates
(
    file_name varbinary(755)             not null,
    file_root varbinary(16)  default '/' not null,
    file_hash varbinary(128) default ''  null,
    file_size bigint                     null,
    mod_time  bigint                     null,
    primary key (file_name, file_root)
);

create index idx_duplicates_file_hash
    on duplicates (file_hash);

create table errors
(
    id            int unsigned auto_increment
        primary key,
    error_time    datetime        null,
    error_level   varbinary(32)   null,
    error_message varbinary(2048) null
);

create index idx_errors_error_time
    on errors (error_time);

create table faces
(
    id               varbinary(64)            not null
        primary key,
    face_src         varbinary(8)             null,
    face_kind        int                      null,
    face_hidden      tinyint(1)               null,
    subj_uid         varbinary(42) default '' null,
    samples          int                      null,
    sample_radius    double                   null,
    collisions       int                      null,
    collision_radius double                   null,
    embedding_json   mediumblob               null,
    matched_at       datetime                 null,
    created_at       datetime                 null,
    updated_at       datetime                 null,
    merge_retry      tinyint(3)    default 0  null,
    merge_notes      varchar(255)  default '' null
);

create index idx_faces_subj_uid
    on faces (subj_uid);

create table files
(
    id                   int unsigned auto_increment
        primary key,
    photo_id             int unsigned              null,
    photo_uid            varbinary(42)             null,
    photo_taken_at       datetime                  null,
    time_index           varbinary(64)             null,
    media_id             varbinary(32)             null,
    media_utc            bigint                    null,
    instance_id          varbinary(64)             null,
    file_uid             varbinary(42)             null,
    file_name            varbinary(1024)           null,
    file_root            varbinary(16) default '/' null,
    original_name        varbinary(755)            null,
    file_hash            varbinary(128)            null,
    file_size            bigint                    null,
    file_codec           varbinary(32)             null,
    file_type            varbinary(16)             null,
    media_type           varbinary(16)             null,
    file_mime            varbinary(64)             null,
    file_primary         tinyint(1)                null,
    file_sidecar         tinyint(1)                null,
    file_missing         tinyint(1)                null,
    file_portrait        tinyint(1)                null,
    file_video           tinyint(1)                null,
    file_duration        bigint                    null,
    file_fps             double                    null,
    file_frames          int                       null,
    file_pages           int           default 0   null,
    file_width           int                       null,
    file_height          int                       null,
    file_orientation     int                       null,
    file_orientation_src varbinary(8)  default ''  null,
    file_projection      varbinary(64)             null,
    file_aspect_ratio    float                     null,
    file_hdr             tinyint(1)                null,
    file_watermark       tinyint(1)                null,
    file_color_profile   varbinary(64)             null,
    file_main_color      varbinary(16)             null,
    file_colors          varbinary(18)             null,
    File_luminance       varbinary(18)             null,
    file_diff            int           default -1  null,
    file_chroma          smallint      default -1  null,
    file_software        varchar(64)               null,
    file_error           varbinary(512)            null,
    mod_time             bigint                    null,
    created_at           datetime                  null,
    created_in           bigint                    null,
    updated_at           datetime                  null,
    updated_in           bigint                    null,
    published_at         datetime                  null,
    deleted_at           datetime                  null,
    constraint idx_files_name_root
        unique (file_name, file_root),
    constraint idx_files_search_media
        unique (media_id),
    constraint idx_files_search_timeline
        unique (time_index),
    constraint uix_files_file_uid
        unique (file_uid)
);

create index idx_files_deleted_at
    on files (deleted_at);

create index idx_files_file_error
    on files (file_error);

create index idx_files_file_hash
    on files (file_hash);

create index idx_files_instance_id
    on files (instance_id);

create index idx_files_media_utc
    on files (media_utc);

create index idx_files_missing_root
    on files (file_missing, file_root);

create index idx_files_photo_id
    on files (photo_id, file_primary);

create index idx_files_photo_taken_at
    on files (photo_taken_at);

create index idx_files_photo_uid
    on files (photo_uid);

create index idx_files_published_at
    on files (published_at);

create table files_share
(
    file_id     int unsigned   not null,
    service_id  int unsigned   not null,
    remote_name varbinary(255) not null,
    status      varbinary(16)  null,
    error       varbinary(512) null,
    errors      int            null,
    created_at  datetime       null,
    updated_at  datetime       null,
    primary key (file_id, service_id, remote_name)
);

create table files_sync
(
    remote_name varbinary(255) not null,
    service_id  int unsigned   not null,
    file_id     int unsigned   null,
    remote_date datetime       null,
    remote_size bigint         null,
    status      varbinary(16)  null,
    error       varbinary(512) null,
    errors      int            null,
    created_at  datetime       null,
    updated_at  datetime       null,
    primary key (remote_name, service_id)
);

create index idx_files_sync_file_id
    on files_sync (file_id);

create table folders
(
    path               varbinary(1024)            null,
    root               varbinary(16) default ''   null,
    folder_uid         varbinary(42)              not null
        primary key,
    folder_type        varbinary(16)              null,
    folder_title       varchar(200)               null,
    folder_category    varchar(100)               null,
    folder_description varchar(2048)              null,
    folder_order       varbinary(32)              null,
    folder_country     varbinary(2)  default 'zz' null,
    folder_year        int                        null,
    folder_month       int                        null,
    folder_day         int                        null,
    folder_favorite    tinyint(1)                 null,
    folder_private     tinyint(1)                 null,
    folder_ignore      tinyint(1)                 null,
    folder_watch       tinyint(1)                 null,
    created_at         datetime                   null,
    updated_at         datetime                   null,
    modified_at        datetime                   null,
    published_at       datetime                   null,
    deleted_at         datetime                   null,
    constraint idx_folders_path_root
        unique (path, root)
);

create index idx_folders_country_year_month
    on folders (folder_country, folder_year, folder_month);

create index idx_folders_deleted_at
    on folders (deleted_at);

create index idx_folders_folder_category
    on folders (folder_category);

create index idx_folders_published_at
    on folders (published_at);

create table keywords
(
    id      int unsigned auto_increment
        primary key,
    keyword varchar(64) null,
    skip    tinyint(1)  null
);

create index idx_keywords_keyword
    on keywords (keyword);

create table labels
(
    id                int unsigned auto_increment
        primary key,
    label_uid         varbinary(42)             null,
    label_slug        varbinary(160)            null,
    custom_slug       varbinary(160)            null,
    label_name        varchar(160)              null,
    label_priority    int                       null,
    label_favorite    tinyint(1)                null,
    label_description varchar(2048)             null,
    label_notes       varchar(1024)             null,
    photo_count       int            default 1  null,
    thumb             varbinary(128) default '' null,
    thumb_src         varbinary(8)   default '' null,
    created_at        datetime                  null,
    updated_at        datetime                  null,
    published_at      datetime                  null,
    deleted_at        datetime                  null,
    label_nsfw        tinyint(1)     default 0  null,
    constraint uix_labels_label_slug
        unique (label_slug),
    constraint uix_labels_label_uid
        unique (label_uid)
);

create index idx_labels_custom_slug
    on labels (custom_slug);

create index idx_labels_deleted_at
    on labels (deleted_at);

create index idx_labels_published_at
    on labels (published_at);

create index idx_labels_thumb
    on labels (thumb);

create table lenses
(
    id               int unsigned auto_increment
        primary key,
    lens_slug        varbinary(160) null,
    lens_name        varchar(160)   null,
    lens_make        varchar(160)   null,
    lens_model       varchar(160)   null,
    lens_type        varchar(100)   null,
    lens_description varchar(2048)  null,
    lens_notes       varchar(1024)  null,
    created_at       datetime       null,
    updated_at       datetime       null,
    deleted_at       datetime       null,
    constraint uix_lenses_lens_slug
        unique (lens_slug)
);

create index idx_lenses_deleted_at
    on lenses (deleted_at);

create table links
(
    link_uid     varbinary(42)  not null
        primary key,
    share_uid    varbinary(42)  null,
    share_slug   varbinary(160) null,
    link_token   varbinary(160) null,
    link_expires int            null,
    link_views   int unsigned   null,
    max_views    int unsigned   null,
    has_password tinyint(1)     null,
    comment      varchar(512)   null,
    perm         int unsigned   null,
    ref_id       varbinary(16)  null,
    created_by   varbinary(42)  null,
    created_at   datetime       null,
    modified_at  datetime       null,
    constraint idx_links_uid_token
        unique (share_uid, link_token)
);

create index idx_links_created_by
    on links (created_by);

create index idx_links_share_slug
    on links (share_slug);

create table markers
(
    marker_uid      varbinary(42)             not null
        primary key,
    file_uid        varbinary(42)  default '' null,
    marker_type     varbinary(8)   default '' null,
    marker_src      varbinary(8)   default '' null,
    marker_name     varchar(160)              null,
    marker_review   tinyint(1)                null,
    marker_invalid  tinyint(1)                null,
    subj_uid        varbinary(42)             null,
    subj_src        varbinary(8)   default '' null,
    face_id         varbinary(64)             null,
    face_dist       double         default -1 null,
    embeddings_json mediumblob                null,
    landmarks_json  mediumblob                null,
    x               float                     null,
    y               float                     null,
    w               float                     null,
    h               float                     null,
    q               int                       null,
    size            int            default -1 null,
    score           smallint                  null,
    thumb           varbinary(128) default '' null,
    matched_at      datetime                  null,
    created_at      datetime                  null,
    updated_at      datetime                  null
);

create index idx_markers_face_id
    on markers (face_id);

create index idx_markers_file_uid
    on markers (file_uid);

create index idx_markers_matched_at
    on markers (matched_at);

create index idx_markers_subj_uid_src
    on markers (subj_uid, subj_src);

create index idx_markers_thumb
    on markers (thumb);

create table migrations
(
    id          varchar(16)  not null
        primary key,
    dialect     varchar(16)  null,
    stage       varchar(16)  null,
    error       varchar(255) null,
    source      varchar(16)  null,
    started_at  datetime     null,
    finished_at datetime     null
);

create table passcodes
(
    uid           varbinary(255)           not null,
    key_type      varchar(64)   default '' not null,
    key_url       varchar(2048) default '' null,
    recovery_code varchar(255)  default '' null,
    verified_at   datetime                 null,
    activated_at  datetime                 null,
    created_at    datetime                 null,
    updated_at    datetime                 null,
    primary key (uid, key_type)
);

create table passwords
(
    uid        varbinary(255) not null
        primary key,
    hash       varbinary(255) null,
    created_at datetime       null,
    updated_at datetime       null
);

create table photos
(
    id                 int unsigned auto_increment
        primary key,
    uuid               varbinary(64)                 null,
    taken_at           datetime                      null,
    taken_at_local     datetime                      null,
    taken_src          varbinary(8)                  null,
    photo_uid          varbinary(42)                 null,
    photo_type         varbinary(8)  default 'image' null,
    type_src           varbinary(8)                  null,
    photo_title        varchar(200)                  null,
    title_src          varbinary(8)                  null,
    photo_caption      varchar(4096)                 null,
    caption_src        varbinary(8)                  null,
    photo_path         varbinary(1024)               null,
    photo_name         varbinary(255)                null,
    original_name      varbinary(755)                null,
    photo_stack        tinyint                       null,
    photo_favorite     tinyint(1)                    null,
    photo_private      tinyint(1)                    null,
    photo_scan         tinyint(1)                    null,
    photo_panorama     tinyint(1)                    null,
    time_zone          varbinary(64) default 'Local' null,
    place_id           varbinary(42) default 'zz'    null,
    place_src          varbinary(8)                  null,
    cell_id            varbinary(42) default 'zz'    null,
    cell_accuracy      int                           null,
    photo_altitude     int                           null,
    photo_lat          double                        null,
    photo_lng          double                        null,
    photo_country      varbinary(2)  default 'zz'    null,
    photo_year         int                           null,
    photo_month        int                           null,
    photo_day          int                           null,
    photo_iso          int                           null,
    photo_exposure     varbinary(64)                 null,
    photo_f_number     float                         null,
    photo_focal_length int                           null,
    photo_quality      smallint                      null,
    photo_faces        int                           null,
    photo_resolution   smallint                      null,
    photo_duration     bigint                        null,
    photo_color        smallint      default -1      null,
    camera_id          int unsigned  default 1       null,
    camera_serial      varbinary(160)                null,
    camera_src         varbinary(8)                  null,
    lens_id            int unsigned  default 1       null,
    created_by         varbinary(42)                 null,
    created_at         datetime                      null,
    updated_at         datetime                      null,
    edited_at          datetime                      null,
    published_at       datetime                      null,
    checked_at         datetime                      null,
    estimated_at       datetime                      null,
    deleted_at         datetime                      null,
    indexed_at         datetime                      null,
    constraint uix_photos_photo_uid
        unique (photo_uid)
);

create index idx_photos_camera_lens
    on photos (camera_id, lens_id);

create index idx_photos_cell_id
    on photos (cell_id);

create index idx_photos_checked_at
    on photos (checked_at);

create index idx_photos_country_year_month
    on photos (photo_country, photo_year, photo_month);

create index idx_photos_created_by
    on photos (created_by);

create index idx_photos_deleted_at
    on photos (deleted_at);

create index idx_photos_path_name
    on photos (photo_path, photo_name);

create index idx_photos_photo_lat
    on photos (photo_lat);

create index idx_photos_photo_lng
    on photos (photo_lng);

create index idx_photos_place_id
    on photos (place_id);

create index idx_photos_published_at
    on photos (published_at);

create index idx_photos_taken_uid
    on photos (taken_at, photo_uid);

create index idx_photos_uuid
    on photos (uuid);

create index idx_photos_ymd
    on photos (photo_day);

create table photos_albums
(
    photo_uid  varbinary(42) not null,
    album_uid  varbinary(42) not null,
    `order`    int           null,
    hidden     tinyint(1)    null,
    missing    tinyint(1)    null,
    created_at datetime      null,
    updated_at datetime      null,
    primary key (photo_uid, album_uid)
);

create index idx_photos_albums_album_uid
    on photos_albums (album_uid);

create table photos_keywords
(
    photo_id   int unsigned not null,
    keyword_id int unsigned not null,
    primary key (photo_id, keyword_id)
);

create index idx_photos_keywords_keyword_id
    on photos_keywords (keyword_id);

create table photos_labels
(
    photo_id    int unsigned       not null,
    label_id    int unsigned       not null,
    label_src   varbinary(8)       null,
    uncertainty smallint           null,
    topicality  smallint default 0 null,
    nsfw        smallint default 0 null,
    primary key (photo_id, label_id)
);

create index idx_photos_labels_label_id
    on photos_labels (label_id);

create table photos_users
(
    uid      varbinary(42) not null,
    user_uid varbinary(42) not null,
    team_uid varbinary(42) null,
    perm     int unsigned  null,
    primary key (uid, user_uid)
);

create index idx_photos_users_team_uid
    on photos_users (team_uid);

create index idx_photos_users_user_uid
    on photos_users (user_uid);

create table places
(
    id             varbinary(42) not null
        primary key,
    place_label    varchar(400)  null,
    place_district varchar(100)  null,
    place_city     varchar(100)  null,
    place_state    varchar(100)  null,
    place_country  varbinary(2)  null,
    place_keywords varchar(300)  null,
    place_favorite tinyint(1)    null,
    photo_count    int default 1 null,
    created_at     datetime      null,
    updated_at     datetime      null
);

create index idx_places_place_city
    on places (place_city);

create index idx_places_place_district
    on places (place_district);

create index idx_places_place_state
    on places (place_state);

create table reactions
(
    uid        varbinary(42) not null,
    user_uid   varbinary(42) not null,
    reaction   varbinary(64) not null,
    reacted    int           null,
    reacted_at datetime      null,
    primary key (uid, user_uid, reaction)
);

create index idx_reactions_reacted_at
    on reactions (reacted_at);

create table services
(
    id             int unsigned auto_increment
        primary key,
    acc_name       varchar(160)    null,
    acc_owner      varchar(160)    null,
    acc_url        varchar(255)    null,
    acc_type       varbinary(255)  null,
    acc_key        varbinary(255)  null,
    acc_user       varbinary(255)  null,
    acc_pass       varbinary(255)  null,
    acc_timeout    varbinary(16)   null,
    acc_error      varbinary(512)  null,
    acc_errors     int             null,
    acc_share      tinyint(1)      null,
    acc_sync       tinyint(1)      null,
    retry_limit    int             null,
    share_path     varbinary(1024) null,
    share_size     varbinary(16)   null,
    share_expires  int             null,
    sync_path      varbinary(1024) null,
    sync_status    varbinary(16)   null,
    sync_interval  int             null,
    sync_date      datetime        null,
    sync_upload    tinyint(1)      null,
    sync_download  tinyint(1)      null,
    sync_filenames tinyint(1)      null,
    sync_raw       tinyint(1)      null,
    created_at     datetime        null,
    updated_at     datetime        null,
    deleted_at     datetime        null
);

create index idx_services_deleted_at
    on services (deleted_at);

create table subjects
(
    subj_uid      varbinary(42)             not null
        primary key,
    subj_type     varbinary(8)   default '' null,
    subj_src      varbinary(8)   default '' null,
    subj_slug     varbinary(160) default '' null,
    subj_name     varchar(160)   default '' null,
    subj_alias    varchar(160)   default '' null,
    subj_about    varchar(512)              null,
    subj_bio      varchar(2048)             null,
    subj_notes    varchar(1024)             null,
    subj_favorite tinyint(1)     default 0  null,
    subj_hidden   tinyint(1)     default 0  null,
    subj_private  tinyint(1)     default 0  null,
    subj_excluded tinyint(1)     default 0  null,
    file_count    int            default 0  null,
    photo_count   int            default 0  null,
    thumb         varbinary(128) default '' null,
    thumb_src     varbinary(8)   default '' null,
    created_at    datetime                  null,
    updated_at    datetime                  null,
    deleted_at    datetime                  null,
    constraint uix_subjects_subj_name
        unique (subj_name)
);

create index idx_subjects_deleted_at
    on subjects (deleted_at);

create index idx_subjects_subj_slug
    on subjects (subj_slug);

create index idx_subjects_thumb
    on subjects (thumb);

create table versions
(
    id          int unsigned auto_increment
        primary key,
    version     varchar(255) null,
    edition     varchar(255) null,
    error       varchar(255) null,
    created_at  datetime     null,
    updated_at  datetime     null,
    migrated_at datetime     null,
    constraint idx_version_edition
        unique (version, edition)
);

